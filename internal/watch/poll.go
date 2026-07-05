package watch

import "github.com/rannday/bbrs/internal/syncer"

// FileState tracks one source file in a snapshot.
type FileState struct {
	Size    int64
	ModTime int64
	Matched bool
}

// Snapshot maps cleaned relative paths to file state.
type Snapshot map[string]FileState

// SnapshotSource captures the current source tree state.
func SnapshotSource(source string, patterns syncer.Patterns, ignored syncer.IgnoredDirs) (Snapshot, error) {
	snapshot := make(Snapshot)
	err := syncer.WalkSource(source, patterns, ignored, func(entry syncer.SourceEntry) error {
		snapshot[entry.Relative] = FileState{
			Size:    entry.Info.Size(),
			ModTime: entry.Info.ModTime().UnixNano(),
			Matched: patterns.Match(entry.Relative),
		}
		return nil
	})
	return snapshot, err
}

// DiffSnapshots returns modified and deleted relative paths with matched-file relevance.
func DiffSnapshots(previous, current Snapshot) syncer.ChangeSet {
	if previous == nil {
		return syncer.ChangeSet{}
	}

	keys := make(map[string]struct{}, len(previous)+len(current))
	for key := range previous {
		keys[key] = struct{}{}
	}
	for key := range current {
		keys[key] = struct{}{}
	}

	changes := syncer.ChangeSet{}
	for key := range keys {
		oldState, hadOld := previous[key]
		newState, hasNew := current[key]
		if hadOld && hasNew && oldState == newState {
			continue
		}
		if !oldState.Matched && !newState.Matched {
			continue
		}
		if hasNew {
			changes.Modified = append(changes.Modified, key)
		} else if hadOld {
			changes.Deleted = append(changes.Deleted, key)
		}
	}
	return changes
}

// HasRelevantChange reports whether any matched file changed between snapshots.
func HasRelevantChange(previous, current Snapshot) bool {
	changes := DiffSnapshots(previous, current)
	return len(changes.Modified) > 0 || len(changes.Deleted) > 0
}
