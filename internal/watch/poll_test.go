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
	snapshot, err := SnapshotSource(root, patterns)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var changes atomic.Int32
	done := make(chan struct{})
	go func() {
		_ = Poll(ctx, root, patterns, 20*time.Millisecond, 10*time.Millisecond, func() {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var changes atomic.Int32
	done := make(chan struct{})
	go func() {
		_ = Poll(ctx, root, patterns, 20*time.Millisecond, 10*time.Millisecond, func() {
			changes.Add(1)
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}