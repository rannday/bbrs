package watch

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rannday/bbrs/internal/syncer"
)

func TestHasRelevantChangeDetectsCreateModifyDelete(t *testing.T) {
	previous := Snapshot{"main.js": {Size: 1, ModTime: 1, Matched: true}}
	if HasRelevantChange(nil, previous) {
		t.Fatal("nil previous should not count as a change")
	}
	if !HasRelevantChange(previous, Snapshot{"main.js": {Size: 2, ModTime: 2, Matched: true}}) {
		t.Fatal("modify not detected")
	}
	if !HasRelevantChange(previous, Snapshot{}) {
		t.Fatal("delete not detected")
	}
}

func TestDiffSnapshotsReportsModifiedAndDeleted(t *testing.T) {
	previous := Snapshot{"main.js": {Size: 1, ModTime: 1, Matched: true}}
	changes := DiffSnapshots(previous, Snapshot{"main.js": {Size: 2, ModTime: 2, Matched: true}})
	if len(changes.Modified) != 1 || changes.Modified[0] != "main.js" {
		t.Fatalf("modified = %#v", changes.Modified)
	}

	changes = DiffSnapshots(previous, Snapshot{})
	if len(changes.Deleted) != 1 || changes.Deleted[0] != "main.js" {
		t.Fatalf("deleted = %#v", changes.Deleted)
	}
}

func TestHasRelevantChangeIgnoresUnmatchedFiles(t *testing.T) {
	previous := Snapshot{"README.md": {Size: 1, ModTime: 1, Matched: false}}
	current := Snapshot{"README.md": {Size: 2, ModTime: 2, Matched: false}}
	if HasRelevantChange(previous, current) {
		t.Fatal("unmatched change detected")
	}
}

func TestSnapshotSourceUsesCleanedRelativePaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.js"), "ok")

	patterns, err := syncer.NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotSource(root, patterns, mustIgnored(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot["src/main.js"]; !ok {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestPollSeedsBaselineWithoutTriggeringChange(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.js"), "ok")

	patterns, err := syncer.NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	ignored := mustIgnored(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var changes atomic.Int32
	done := make(chan struct{})
	go func() {
		_ = Poll(ctx, root, patterns, ignored, 20*time.Millisecond, 10*time.Millisecond, func(syncer.ChangeSet) {
			changes.Add(1)
		})
		close(done)
	}()

	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	if got := changes.Load(); got != 0 {
		t.Fatalf("changes = %d, want 0", got)
	}
}

func TestPollDetectsMatchedFileChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.js")
	writeFile(t, path, "v1")

	patterns, err := syncer.NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	ignored := mustIgnored(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var changes atomic.Int32
	done := make(chan struct{})
	go func() {
		_ = Poll(ctx, root, patterns, ignored, 20*time.Millisecond, 10*time.Millisecond, func(changeset syncer.ChangeSet) {
			if len(changeset.Modified) > 0 || len(changeset.Deleted) > 0 {
				changes.Add(1)
			}
		})
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	writeFile(t, path, "v2")

	deadline := time.Now().Add(500 * time.Millisecond)
	for changes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	if got := changes.Load(); got != 1 {
		t.Fatalf("changes = %d, want 1", got)
	}
}

func TestIgnoredPathChecksParentDirectories(t *testing.T) {
	ignored := mustIgnored(t, []string{"vendor/generated"})
	for _, relative := range []string{
		"dist/main.js",
		"vendor/generated/main.js",
	} {
		if !ignoredPath(relative, ignored) {
			t.Fatalf("%s was not ignored", relative)
		}
	}
	if ignoredPath("src/main.js", ignored) {
		t.Fatal("src/main.js was ignored")
	}
}

func mustIgnored(t *testing.T, extra []string) syncer.IgnoredPatterns {
	t.Helper()
	ignored, err := syncer.NewIgnoredPatterns(extra)
	if err != nil {
		t.Fatal(err)
	}
	return ignored
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
