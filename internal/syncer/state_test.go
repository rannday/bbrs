package syncer

import "testing"

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