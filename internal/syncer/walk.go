package syncer

import (
	"io/fs"
	"path/filepath"
)

type SourceEntry struct {
	SourcePath string
	Relative   string
	Info       fs.FileInfo
}

func WalkSource(source string, patterns Patterns, visit func(SourceEntry) error) error {
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
		return visit(SourceEntry{
			SourcePath: current,
			Relative:   relative,
			Info:       info,
		})
	})
}