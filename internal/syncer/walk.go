package syncer

import (
	"io/fs"
	"path/filepath"
)

// SourceEntry is one regular file discovered under the source directory.
type SourceEntry struct {
	SourcePath string
	Relative   string
	Info       fs.FileInfo
}

// WalkSource visits regular files under source, skipping ignored paths and symlinks.
func WalkSource(source string, patterns Patterns, ignored IgnoredPatterns, visit func(SourceEntry) error) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if current != source {
				relativePath, err := filepath.Rel(source, current)
				if err != nil {
					return err
				}
				relative, err := cleanRelativeSlashPath(relativePath)
				if err != nil {
					return err
				}
				if ignored.IsIgnored(relative) {
					return filepath.SkipDir
				}
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
		if ignored.IsIgnored(relative) {
			return nil
		}
		return visit(SourceEntry{
			SourcePath: current,
			Relative:   relative,
			Info:       info,
		})
	})
}
