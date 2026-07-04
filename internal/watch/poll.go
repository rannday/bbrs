package watch

import (
	"context"
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
	err := syncer.WalkSource(source, patterns, func(entry syncer.SourceEntry) error {
		snapshot[entry.Relative] = FileState{
			Size:    entry.Info.Size(),
			ModTime: entry.Info.ModTime().UnixNano(),
			Matched: patterns.Match(entry.Relative),
		}
		return nil
	})
	return snapshot, err
}

func HasRelevantChange(previous, current Snapshot) bool {
	if previous == nil {
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
		if hadOld && hasNew && oldState == newState {
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
	var seeded bool
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
			if !seeded {
				previous = current
				seeded = true
				continue
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