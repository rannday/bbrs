package syncer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateShouldUploadAndRemember(t *testing.T) {
	state := NewState()
	stamp := FileStamp{Size: 10, ModTime: 20}
	if !state.ShouldUpload("main.js", stamp) {
		t.Fatal("expected first upload")
	}
	state.RememberUpload("main.js", stamp)
	if state.ShouldUpload("main.js", stamp) {
		t.Fatal("expected cached upload to be skipped")
	}
	if !state.ShouldUpload("main.js", FileStamp{Size: 11, ModTime: 20}) {
		t.Fatal("expected modified file to upload")
	}
}

func TestStateForgetRemote(t *testing.T) {
	state := NewState()
	stamp := FileStamp{Size: 1, ModTime: 1}
	state.RememberUpload("old.js", stamp)
	state.ForgetRemote("old.js")
	if !state.ShouldUpload("old.js", stamp) {
		t.Fatal("expected cache miss after delete")
	}
}

func TestStateSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	state := NewState()
	state.RememberUpload("main.js", FileStamp{Size: 10, ModTime: 20})
	if err := state.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ShouldUpload("main.js", FileStamp{Size: 10, ModTime: 20}) {
		t.Fatal("expected cached stamp")
	}
	if !loaded.ShouldUpload("main.js", FileStamp{Size: 11, ModTime: 20}) {
		t.Fatal("expected upload for changed stamp")
	}
}

func TestLoadStateMissingFileReturnsEmptyState(t *testing.T) {
	state, err := LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.UploadCache) != 0 {
		t.Fatalf("cache = %#v", state.UploadCache)
	}
}

func TestLoadStateRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"upload_cache":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("expected version error")
	}
}
