package watch

import (
	"context"
	"io/fs"
	"path/filepath"
	"reflect"
	"time"

	"github.com/rannday/bbrs/internal/syncer"
)

type FileState struct {
	Size    int64
	ModTime int64
	Matched bool
}

type Snapshot map[string]FileState

func SnapshotSource(source string, patterns syncer.Patterns) (Snapshot, error) {
	snapshot := make(Snapshot)
	err := filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if current != source && syncer.IsIgnoredDir(entry.Name()) {
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
		relative := syncer.NormalizeSlashes(relativePath)
		snapshot[relative] = FileState{
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
			Matched: patterns.Match(relative),
		}
		return nil
	})
	return snapshot, err
}

func HasRelevantChange(previous, current Snapshot) bool {
	if previous == nil {
		for _, state := range current {
			if state.Matched {
				return true
			}
		}
		return false
	}

	keys := make(map[string]struct{}, len(previous)+len(current))
	for key := range previous {
		keys[key] = struct{}{}
	}
	for key := range current {
		keys[key] = struct{}{}
	}

	for key := range keys {
		oldState, hadOld := previous[key]
		newState, hasNew := current[key]
		if hadOld && hasNew && reflect.DeepEqual(oldState, newState) {
			continue
		}
		if oldState.Matched || newState.Matched {
			return true
		}
	}
	return false
}

func Poll(ctx context.Context, source string, patterns syncer.Patterns, interval, debounce time.Duration, onChange func()) error {
	var previous Snapshot
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return ctx.Err()
		case <-ticker.C:
			current, err := SnapshotSource(source, patterns)
			if err != nil {
				return err
			}
			if HasRelevantChange(previous, current) {
				if debounceTimer == nil {
					debounceTimer = time.NewTimer(debounce)
					debounceC = debounceTimer.C
				} else {
					if !debounceTimer.Stop() {
						select {
						case <-debounceTimer.C:
						default:
						}
					}
					debounceTimer.Reset(debounce)
				}
			}
			previous = current
		case <-debounceC:
			debounceC = nil
			debounceTimer = nil
			onChange()
		}
	}
}
