package syncer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileMetadata describes a remote file returned by getAllFileMetadata.
type FileMetadata struct {
	Filename string            `json:"filename"`
	Atime    FlexibleTimestamp `json:"atime"`
	Btime    FlexibleTimestamp `json:"btime"`
	Mtime    FlexibleTimestamp `json:"mtime"`
}

// RemoteAPI is the Bitburner Remote API surface used by the syncer.
type RemoteAPI interface {
	GetAllFileMetadata(ctx context.Context, server string) ([]FileMetadata, error)
	PushFile(ctx context.Context, server, filename, content string) error
	DeleteFile(ctx context.Context, server, filename string) error
}

// Options configures mirror and incremental sync operations.
type Options struct {
	Source      string
	Destination string
	Host        string
	Patterns    Patterns
	Ignored     IgnoredPatterns
	State       *State
	CachePath   string
}

// DesiredFile is a local file that should exist remotely.
type DesiredFile struct {
	SourcePath string
	Relative   string
	Remote     string
	Stamp      FileStamp
}

// Plan is the upload and delete set for a full mirror.
type Plan struct {
	Desired []DesiredFile
	Deletes []string
	Ignored int
}

// Summary counts sync operations performed or planned.
type Summary struct {
	Uploaded int
	Skipped  int
	Deleted  int
	Ignored  int
	Failed   int
}

// Result combines summary counts with per-file errors from best-effort sync.
type Result struct {
	Summary Summary
	Errors  []error
}

// ChangeSet lists relative source paths changed or deleted since last watch event.
type ChangeSet struct {
	Modified []string
	Deleted  []string
}

// RemoteNames extracts filenames from remote metadata.
func RemoteNames(metadata []FileMetadata) []string {
	names := make([]string, 0, len(metadata))
	for _, entry := range metadata {
		names = append(names, entry.Filename)
	}
	return names
}

// BuildPlan builds the full mirror upload and delete plan.
func BuildPlan(source, destination string, patterns Patterns, ignored IgnoredPatterns, remoteNames []string) (Plan, error) {
	desired, ignoredCount, err := BuildDesired(source, destination, patterns, ignored)
	if err != nil {
		return Plan{}, err
	}
	deletes, err := BuildDeletes(destination, patterns, desired, remoteNames)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Desired: desired,
		Deletes: deletes,
		Ignored: ignoredCount,
	}, nil
}

// BuildDeletes returns stale remote files under destination that match patterns.
func BuildDeletes(destination string, patterns Patterns, desired []DesiredFile, remoteNames []string) ([]string, error) {
	desiredSet := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		desiredSet[file.Remote] = struct{}{}
	}

	dest, err := NormalizeRemotePath(destination)
	if err != nil {
		return nil, fmt.Errorf("invalid destination %q: %w", destination, err)
	}

	deletes := make([]string, 0)
	for _, remoteName := range remoteNames {
		relative, inside := RelativeToDestination(dest, remoteName)
		if !inside || !patterns.Match(relative) {
			continue
		}
		remote, err := NormalizeRemoteFilePath(remoteName)
		if err != nil {
			continue
		}
		if _, ok := desiredSet[remote]; !ok {
			deletes = append(deletes, remote)
		}
	}
	sort.Strings(deletes)
	return deletes, nil
}

// BuildDesired walks source and returns files that should exist remotely.
func BuildDesired(source, destination string, patterns Patterns, ignored IgnoredPatterns) ([]DesiredFile, int, error) {
	files := make([]DesiredFile, 0)
	ignoredCount := 0

	err := WalkSource(source, patterns, ignored, func(entry SourceEntry) error {
		if !patterns.Match(entry.Relative) {
			ignoredCount++
			return nil
		}
		remote, err := JoinDestinationFile(destination, entry.Relative)
		if err != nil {
			return err
		}
		files = append(files, DesiredFile{
			SourcePath: entry.SourcePath,
			Relative:   entry.Relative,
			Remote:     remote,
			Stamp:      FileStampFromInfo(entry.Info),
		})
		return nil
	})
	if err != nil {
		return nil, ignoredCount, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Remote < files[j].Remote
	})
	return files, ignoredCount, nil
}

// Mirror performs a full best-effort mirror sync.
func Mirror(ctx context.Context, api RemoteAPI, options Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	remoteMetadata, err := api.GetAllFileMetadata(ctx, options.Host)
	if err != nil {
		return Result{}, fmt.Errorf("get remote file metadata: %w", err)
	}

	plan, err := BuildPlan(options.Source, options.Destination, options.Patterns, options.Ignored, RemoteNames(remoteMetadata))
	if err != nil {
		return Result{}, err
	}

	remotePresent := remotePresentSet(remoteMetadata)
	result := applyUploads(ctx, api, options, plan.Desired, remotePresent)
	deleteResult := applyDeletes(ctx, api, options, plan.Deletes)
	result.Summary.Deleted = deleteResult.Summary.Deleted
	result.Summary.Failed += deleteResult.Summary.Failed
	result.Errors = append(result.Errors, deleteResult.Errors...)
	result.Summary.Ignored = plan.Ignored
	return result, nil
}

// SyncChanges applies incremental uploads and deletes for known local changes.
// Falls back to full mirror when changes are empty or incremental work fails early.
func SyncChanges(ctx context.Context, api RemoteAPI, options Options, changes ChangeSet) (Result, error) {
	if len(changes.Modified) == 0 && len(changes.Deleted) == 0 {
		return Mirror(ctx, api, options)
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	modified := uniqueSorted(changes.Modified)
	deleted := uniqueSorted(changes.Deleted)

	desired := make([]DesiredFile, 0, len(modified))
	for _, relative := range modified {
		if !options.Patterns.Match(relative) {
			continue
		}
		sourcePath := filepath.Join(options.Source, filepath.FromSlash(relative))
		info, err := os.Stat(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Result{}, fmt.Errorf("stat %s: %w", sourcePath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		remote, err := JoinDestinationFile(options.Destination, relative)
		if err != nil {
			return Result{}, err
		}
		desired = append(desired, DesiredFile{
			SourcePath: sourcePath,
			Relative:   relative,
			Remote:     remote,
			Stamp:      FileStampFromInfo(info),
		})
	}

	remoteDeletes := make([]string, 0, len(deleted))
	for _, relative := range deleted {
		if !options.Patterns.Match(relative) {
			continue
		}
		remote, err := JoinDestinationFile(options.Destination, relative)
		if err != nil {
			return Result{}, err
		}
		remoteDeletes = append(remoteDeletes, remote)
	}

	// Incremental uploads still need remote presence for skip logic.
	remoteMetadata, err := api.GetAllFileMetadata(ctx, options.Host)
	if err != nil {
		return Result{}, fmt.Errorf("get remote file metadata: %w", err)
	}
	remotePresent := remotePresentSet(remoteMetadata)

	result := applyUploads(ctx, api, options, desired, remotePresent)
	deleteResult := applyDeletes(ctx, api, options, remoteDeletes)
	result.Summary.Deleted = deleteResult.Summary.Deleted
	result.Summary.Failed += deleteResult.Summary.Failed
	result.Errors = append(result.Errors, deleteResult.Errors...)
	return result, nil
}

func applyUploads(ctx context.Context, api RemoteAPI, options Options, desired []DesiredFile, remotePresent map[string]struct{}) Result {
	result := Result{Summary: Summary{}}
	dirty := false

	for _, file := range desired {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err)
			break
		}
		_, present := remotePresent[file.Remote]
		if present && options.State != nil && !options.State.ShouldUpload(file.Remote, file.Stamp) {
			result.Summary.Skipped++
			continue
		}
		content, err := os.ReadFile(file.SourcePath)
		if err != nil {
			result.Summary.Failed++
			result.Errors = append(result.Errors, fmt.Errorf("read %s: %w", file.SourcePath, err))
			continue
		}
		if err := api.PushFile(ctx, options.Host, file.Remote, string(content)); err != nil {
			result.Summary.Failed++
			result.Errors = append(result.Errors, fmt.Errorf("upload %s: %w", file.Remote, err))
			continue
		}
		if options.State != nil {
			options.State.RememberUpload(file.Remote, file.Stamp)
			dirty = true
		}
		result.Summary.Uploaded++
	}

	if dirty {
		result.Errors = append(result.Errors, saveCache(options)...)
	}
	return result
}

func applyDeletes(ctx context.Context, api RemoteAPI, options Options, deletes []string) Result {
	result := Result{Summary: Summary{}}
	dirty := false

	for _, remote := range deletes {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err)
			break
		}
		if err := api.DeleteFile(ctx, options.Host, remote); err != nil {
			result.Summary.Failed++
			result.Errors = append(result.Errors, fmt.Errorf("delete %s: %w", remote, err))
			continue
		}
		if options.State != nil {
			options.State.ForgetRemote(remote)
			dirty = true
		}
		result.Summary.Deleted++
	}

	if dirty {
		result.Errors = append(result.Errors, saveCache(options)...)
	}
	return result
}

func saveCache(options Options) []error {
	if options.State == nil || options.CachePath == "" {
		return nil
	}
	if err := options.State.Save(options.CachePath); err != nil {
		return []error{fmt.Errorf("save cache: %w", err)}
	}
	return nil
}

func remotePresentSet(metadata []FileMetadata) map[string]struct{} {
	present := make(map[string]struct{}, len(metadata))
	for _, entry := range metadata {
		remote, err := NormalizeRemoteFilePath(entry.Filename)
		if err != nil {
			continue
		}
		present[remote] = struct{}{}
	}
	return present
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = NormalizeSlashes(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
