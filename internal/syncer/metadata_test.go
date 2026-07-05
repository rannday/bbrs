package syncer

import (
	"encoding/json"
	"testing"
)

func TestFileMetadataUnmarshalsGameTimestamps(t *testing.T) {
	const payload = `[{"filename":"hacknet.js","atime":1700000000000,"btime":1700000000000,"mtime":1700000001000}]`

	var metadata []FileMetadata
	if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 1 {
		t.Fatalf("metadata = %#v", metadata)
	}
	if metadata[0].Filename != "hacknet.js" {
		t.Fatalf("filename = %q", metadata[0].Filename)
	}
	if metadata[0].Mtime.Milliseconds() != 1700000001000 {
		t.Fatalf("mtime = %d", metadata[0].Mtime.Milliseconds())
	}
}

func TestFileMetadataUnmarshalsStringTimestamps(t *testing.T) {
	const payload = `[{"filename":"main.js","atime":"1700000000000","btime":"1700000000000","mtime":"1700000001000"}]`

	var metadata []FileMetadata
	if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata[0].Mtime.Milliseconds() != 1700000001000 {
		t.Fatalf("mtime = %d", metadata[0].Mtime.Milliseconds())
	}
}

func TestFlexibleTimestampNormalizesSeconds(t *testing.T) {
	const payload = `[{"filename":"main.js","atime":1700000000,"btime":1700000000,"mtime":1700000001}]`

	var metadata []FileMetadata
	if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata[0].Mtime.Milliseconds() != 1700000001000 {
		t.Fatalf("mtime = %d", metadata[0].Mtime.Milliseconds())
	}
}
