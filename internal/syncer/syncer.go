package syncer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

type RemoteAPI interface {
	GetFileNames(ctx context.Context, server string) ([]string, error)
	PushFile(ctx context.Context, server, filename, content string) error
	DeleteFile(ctx context.Context, server, filename string) error
}

type Options struct {
	Source      string
	Destination string
	Host        string
	Patterns    Patterns
}

type DesiredFile struct {
	SourcePath string
	Relative   string
	Remote     string
}

type Plan struct {
	Desired []DesiredFile
	Deletes []string
	Ignored int
}

type Summary struct {
	Uploaded int
	Deleted  int
	Ignored  int
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

	err := filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if current != source && IsIgnoredDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		relativePath, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		relative, err := cleanRelativeSlashPath(relativePath)
		if err != nil {
			return err
		}
		if !patterns.Match(relative) {
			ignored++
			return nil
		}
		remote, err := JoinDestinationFile(destination, relative)
		if err != nil {
			return err
		}
		files = append(files, DesiredFile{
			SourcePath: current,
			Relative:   relative,
			Remote:     remote,
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
	desired, ignored, err := BuildDesired(options.Source, options.Destination, options.Patterns)
	if err != nil {
		return Summary{}, err
	}

	remoteNames, err := api.GetFileNames(ctx, options.Host)
	if err != nil {
		return Summary{}, fmt.Errorf("get remote file names: %w", err)
	}
	deletes, err := BuildDeletes(options.Destination, options.Patterns, desired, remoteNames)
	if err != nil {
		return Summary{}, err
	}

	uploaded := 0
	for _, file := range desired {
		content, err := os.ReadFile(file.SourcePath)
		if err != nil {
			return Summary{}, fmt.Errorf("read %s: %w", file.SourcePath, err)
		}
		if err := api.PushFile(ctx, options.Host, file.Remote, string(content)); err != nil {
			return Summary{}, fmt.Errorf("upload %s: %w", file.Remote, err)
		}
		uploaded++
	}

	deleted := 0
	for _, remote := range deletes {
		if err := api.DeleteFile(ctx, options.Host, remote); err != nil {
			return Summary{}, fmt.Errorf("delete %s: %w", remote, err)
		}
		deleted++
	}

	return Summary{
		Uploaded: uploaded,
		Deleted:  deleted,
		Ignored:  ignored,
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
