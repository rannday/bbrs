package watch

import "testing"

func TestHasRelevantChangeDetectsCreateModifyDelete(t *testing.T) {
	previous := Snapshot{"main.js": {Size: 1, ModTime: 1, Matched: true}}
	if !HasRelevantChange(nil, previous) {
		t.Fatal("initial load not detected")
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
