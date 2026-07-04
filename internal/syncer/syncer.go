package syncer

import (
	"context"
	"fmt"
	"os"
	"sort"
)

var IgnoredDirNames = []string{
	".git",
	"target",
	"node_modules",
	"dist",
	"build",
	".zed",
	".vscode",
	".idea",
	"coverage",
	"tmp",
	"temp",
}

type FileMetadata struct {
	Filename string `json:"filename"`
	// Bitburner sends Unix epoch milliseconds. The remote_api.md prose docs
	// describe these as strings, but the game API uses numbers.
	Atime int64 `json:"atime"`
	Btime int64 `json:"btime"`
	Mtime int64 `json:"mtime"`
}

type RemoteAPI interface {
	GetAllFileMetadata(ctx context.Context, server string) ([]FileMetadata, error)
	PushFile(ctx context.Context, server, filename, content string) error
	DeleteFile(ctx context.Context, server, filename string) error
}

type Options struct {
	Source      string
	Destination string
	Host        string
	Patterns    Patterns
	State       *State
}

type DesiredFile struct {
	SourcePath string
	Relative   string
	Remote     string
	Stamp      FileStamp
}

type Plan struct {
	Desired []DesiredFile
	Deletes []string
	Ignored int
}

type Summary struct {
	Uploaded int
	Skipped  int
	Deleted  int
	Ignored  int
}

func RemoteNames(metadata []FileMetadata) []string {
	names := make([]string, 0, len(metadata))
	for _, entry := range metadata {
		names = append(names, entry.Filename)
	}
	return names
}

func BuildPlan(source, destination string, patterns Patterns, remoteNames []string) (Plan, error) {
	desired, ignored, err := BuildDesired(source, destination, patterns)
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
		Ignored: ignored,
	}, nil
}

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

func BuildDesired(source, destination string, patterns Patterns) ([]DesiredFile, int, error) {
	files := make([]DesiredFile, 0)
	ignored := 0

	err := WalkSource(source, patterns, func(entry SourceEntry) error {
		if !patterns.Match(entry.Relative) {
			ignored++
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
		return nil, ignored, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Remote < files[j].Remote
	})
	return files, ignored, nil
}

func Mirror(ctx context.Context, api RemoteAPI, options Options) (Summary, error) {
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}

	remoteMetadata, err := api.GetAllFileMetadata(ctx, options.Host)
	if err != nil {
		return Summary{}, fmt.Errorf("get remote file metadata: %w", err)
	}

	plan, err := BuildPlan(options.Source, options.Destination, options.Patterns, RemoteNames(remoteMetadata))
	if err != nil {
		return Summary{}, err
	}

	uploaded := 0
	skipped := 0
	for _, file := range plan.Desired {
		if err := ctx.Err(); err != nil {
			return Summary{}, err
		}
		if options.State != nil && !options.State.ShouldUpload(file.Remote, file.Stamp) {
			skipped++
			continue
		}
		content, err := os.ReadFile(file.SourcePath)
		if err != nil {
			return Summary{}, fmt.Errorf("read %s: %w", file.SourcePath, err)
		}
		if err := api.PushFile(ctx, options.Host, file.Remote, string(content)); err != nil {
			return Summary{}, fmt.Errorf("upload %s: %w", file.Remote, err)
		}
		if options.State != nil {
			options.State.RememberUpload(file.Remote, file.Stamp)
		}
		uploaded++
	}

	deleted := 0
	for _, remote := range plan.Deletes {
		if err := ctx.Err(); err != nil {
			return Summary{}, err
		}
		if err := api.DeleteFile(ctx, options.Host, remote); err != nil {
			return Summary{}, fmt.Errorf("delete %s: %w", remote, err)
		}
		if options.State != nil {
			options.State.ForgetRemote(remote)
		}
		deleted++
	}

	return Summary{
		Uploaded: uploaded,
		Skipped:  skipped,
		Deleted:  deleted,
		Ignored:  plan.Ignored,
	}, nil
}

func IsIgnoredDir(name string) bool {
	for _, ignored := range IgnoredDirNames {
		if equalFoldASCII(name, ignored) {
			return true
		}
	}
	return false
}

func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		l := left[i]
		r := right[i]
		if l >= 'A' && l <= 'Z' {
			l += 'a' - 'A'
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}