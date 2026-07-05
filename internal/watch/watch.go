package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rannday/bbrs/internal/syncer"
)

// ChangeHandler receives debounced local file change notifications.
type ChangeHandler func(changes syncer.ChangeSet)

// Poll watches source for matching file changes using fsnotify with polling fallback.
// onChange receives debounced ChangeSet values; empty sets mean "rescan everything".
func Poll(ctx context.Context, source string, patterns syncer.Patterns, ignored syncer.IgnoredPatterns, interval, debounce time.Duration, onChange ChangeHandler) error {
	if onChange == nil {
		onChange = func(syncer.ChangeSet) {}
	}

	watcher, err := fsnotify.NewWatcher()
	useNotify := err == nil
	if useNotify {
		defer watcher.Close()
		if err := addWatchTree(watcher, source, ignored); err != nil {
			useNotify = false
			_ = watcher.Close()
		}
	}

	var previous Snapshot
	var seeded bool
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time
	pending := syncer.ChangeSet{}

	flush := func() {
		changes := pending
		pending = syncer.ChangeSet{}
		if !seeded || (len(changes.Modified) == 0 && len(changes.Deleted) == 0) {
			onChange(syncer.ChangeSet{})
			return
		}
		onChange(changes)
	}

	schedule := func(changes syncer.ChangeSet) {
		pending.Modified = append(pending.Modified, changes.Modified...)
		pending.Deleted = append(pending.Deleted, changes.Deleted...)
		if debounceTimer == nil {
			debounceTimer = time.NewTimer(debounce)
			debounceC = debounceTimer.C
			return
		}
		if !debounceTimer.Stop() {
			select {
			case <-debounceTimer.C:
			default:
			}
		}
		debounceTimer.Reset(debounce)
	}

	record := func(changes syncer.ChangeSet) {
		if !seeded {
			return
		}
		if len(changes.Modified) == 0 && len(changes.Deleted) == 0 {
			return
		}
		schedule(changes)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !useNotify || !ok {
				continue
			}
			changes, err := changesFromEvent(source, patterns, ignored, event)
			if err != nil {
				return err
			}
			record(changes)
			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					_ = addWatchTree(watcher, event.Name, ignored)
				}
			}

		case err, ok := <-watcher.Errors:
			if !useNotify || !ok {
				continue
			}
			return err

		case <-ticker.C:
			current, err := SnapshotSource(source, patterns, ignored)
			if err != nil {
				return err
			}
			if !seeded {
				previous = current
				seeded = true
				continue
			}
			record(DiffSnapshots(previous, current))
			previous = current

		case <-debounceC:
			debounceC = nil
			debounceTimer = nil
			flush()
		}
	}
}

func addWatchTree(watcher *fsnotify.Watcher, root string, ignored syncer.IgnoredPatterns) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if ignored.IsIgnored(filepath.ToSlash(rel)) {
				return filepath.SkipDir
			}
		}
		if err := watcher.Add(path); err != nil {
			return err
		}
		return nil
	})
}

func changesFromEvent(source string, patterns syncer.Patterns, ignored syncer.IgnoredPatterns, event fsnotify.Event) (syncer.ChangeSet, error) {
	rel, err := relativeClean(source, event.Name)
	if err != nil {
		return syncer.ChangeSet{}, err
	}
	if rel == "" {
		return syncer.ChangeSet{}, nil
	}
	if ignoredPath(rel, ignored) {
		return syncer.ChangeSet{}, nil
	}

	switch {
	case event.Op&fsnotify.Remove != 0, event.Op&fsnotify.Rename != 0:
		if patterns.Match(rel) {
			return syncer.ChangeSet{Deleted: []string{rel}}, nil
		}
		return syncer.ChangeSet{}, nil
	case event.Op&(fsnotify.Write|fsnotify.Create) != 0:
		info, err := os.Stat(event.Name)
		if err != nil {
			if os.IsNotExist(err) {
				if patterns.Match(rel) {
					return syncer.ChangeSet{Deleted: []string{rel}}, nil
				}
				return syncer.ChangeSet{}, nil
			}
			return syncer.ChangeSet{}, err
		}
		if !info.Mode().IsRegular() {
			return syncer.ChangeSet{}, nil
		}
		if patterns.Match(rel) {
			return syncer.ChangeSet{Modified: []string{rel}}, nil
		}
		return syncer.ChangeSet{}, nil
	default:
		return syncer.ChangeSet{}, nil
	}
}

func relativeClean(source, path string) (string, error) {
	rel, err := filepath.Rel(source, path)
	if err != nil {
		return "", err
	}
	return syncer.NormalizeSlashes(filepath.ToSlash(rel)), nil
}

func ignoredPath(relative string, ignored syncer.IgnoredPatterns) bool {
	if ignored.IsIgnored(relative) {
		return true
	}
	parts := strings.Split(relative, "/")
	current := ""
	for _, part := range parts[:len(parts)-1] {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		if ignored.IsIgnored(current) {
			return true
		}
	}
	return false
}
