package syncer

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultPatternsIncludeJSAndTS(t *testing.T) {
	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !patterns.Match("main.js") {
		t.Fatal("default patterns did not include .js")
	}
	if !patterns.Match("main.ts") {
		t.Fatal("default patterns did not include .ts")
	}
}

func TestDefaultPatternsExcludeDeclarationTypes(t *testing.T) {
	patterns, err := NewPatterns([]string{"*.d.ts", "*.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if patterns.Match("types.d.ts") {
		t.Fatal("*.d.ts should always be excluded")
	}
}

func TestPatternExpandsDefaults(t *testing.T) {
	patterns, err := NewPatterns([]string{"*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.js", "main.ts", "notes.txt"} {
		if !patterns.Match(name) {
			t.Fatalf("%s did not match", name)
		}
	}
}

func TestRepeatedPatternsWork(t *testing.T) {
	patterns, err := NewPatterns([]string{"*.script", "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old.script", "notes.txt"} {
		if !patterns.Match(name) {
			t.Fatalf("%s did not match", name)
		}
	}
}

func TestCommaSeparatedPatternsWork(t *testing.T) {
	patterns, err := NewPatterns([]string{"*.script,*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old.script", "notes.txt"} {
		if !patterns.Match(name) {
			t.Fatalf("%s did not match", name)
		}
	}
}

func TestUnsafeRemotePathsRejected(t *testing.T) {
	for _, path := range []string{"/main.js", "../main.js", "scripts/../main.js", `C:\main.js`, "C:/main.js", ""} {
		if _, err := NormalizeRemoteFilePath(path); err == nil {
			t.Fatalf("%q accepted", path)
		}
	}
}

func TestPathNormalizationConvertsBackslashes(t *testing.T) {
	got, err := NormalizeRemoteFilePath(`scripts\batch\main.js`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "scripts/batch/main.js" {
		t.Fatalf("got %q", got)
	}
}

func TestDestinationPathJoining(t *testing.T) {
	cases := []struct {
		destination string
		want        string
	}{
		{"", "main.js"},
		{".", "main.js"},
		{"scripts/", "scripts/main.js"},
		{`scripts\batch`, "scripts/batch/main.js"},
	}
	for _, tc := range cases {
		got, err := JoinDestinationFile(tc.destination, "main.js")
		if err != nil {
			t.Fatalf("%q err = %v", tc.destination, err)
		}
		if got != tc.want {
			t.Fatalf("%q got %q, want %q", tc.destination, got, tc.want)
		}
	}
}

func TestIgnoredDirsSkippedCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "TARGET", "main.js"), "ignored")
	writeFile(t, filepath.Join(root, "Node_Modules", "dep.js"), "ignored")
	writeFile(t, filepath.Join(root, "src", "main.js"), "ok")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	desired, _, err := BuildDesired(root, "", patterns)
	if err != nil {
		t.Fatal(err)
	}
	got := remotePaths(desired)
	want := []string{"src/main.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSymlinkedPathsAreSkipped(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "main.js"), "outside")
	writeFile(t, filepath.Join(root, "src", "main.js"), "ok")

	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlink not supported:", err)
	}

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	desired, _, err := BuildDesired(root, "", patterns)
	if err != nil {
		t.Fatal(err)
	}
	got := remotePaths(desired)
	want := []string{"src/main.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSyncPlanOrderingIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "z.js"), "z")
	writeFile(t, filepath.Join(root, "a.js"), "a")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(root, "scripts", patterns, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := remotePaths(plan.Desired)
	want := []string{"scripts/a.js", "scripts/z.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMirrorDeletesOnlyMatchingStaleFilesUnderDestination(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.js"), "export async function main(ns) {}")
	writeFile(t, filepath.Join(root, "data.json"), "{}")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{
		{Filename: "scripts/main.js"},
		{Filename: "scripts/old.js"},
		{Filename: "outside/old.js"},
		{Filename: "scripts/data.json"},
		{Filename: "scripts/types.d.ts"},
	}}
	summary, err := Mirror(context.Background(), api, Options{
		Source:      root,
		Destination: "scripts",
		Host:        "home",
		Patterns:    patterns,
		State:       NewState(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Uploaded != 1 || summary.Skipped != 0 || summary.Deleted != 1 || summary.Ignored != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if !reflect.DeepEqual(api.deleted, []string{"scripts/old.js"}) {
		t.Fatalf("deleted = %v", api.deleted)
	}
	if !reflect.DeepEqual(api.uploaded, []string{"scripts/main.js"}) {
		t.Fatalf("uploaded = %v", api.uploaded)
	}
}

func TestMirrorSkipsUnchangedUploads(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.js")
	writeFile(t, path, "v1")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{{Filename: "main.js"}}}
	state := NewState()
	options := Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		State:    state,
	}

	first, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Uploaded != 1 || first.Skipped != 0 {
		t.Fatalf("first summary = %+v", first)
	}

	second, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uploaded != 0 || second.Skipped != 1 {
		t.Fatalf("second summary = %+v", second)
	}
	if len(api.uploaded) != 1 {
		t.Fatalf("uploaded = %v", api.uploaded)
	}
}

func TestMirrorReuploadsCachedFileWhenRemoteMissing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.js"), "v1")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{{Filename: "main.js"}}}
	state := NewState()
	options := Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		State:    state,
	}

	first, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Uploaded != 1 || first.Skipped != 0 {
		t.Fatalf("first summary = %+v", first)
	}

	api.remoteMetadata = nil
	second, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uploaded != 1 || second.Skipped != 0 {
		t.Fatalf("second summary = %+v", second)
	}
	if !reflect.DeepEqual(api.uploaded, []string{"main.js", "main.js"}) {
		t.Fatalf("uploaded = %v", api.uploaded)
	}
}

func TestMirrorUploadsAfterLocalModification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.js")
	writeFile(t, path, "v1")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{{Filename: "main.js"}}}
	options := Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		State:    NewState(),
	}
	if _, err := Mirror(context.Background(), api, options); err != nil {
		t.Fatal(err)
	}

	writeFile(t, path, "v2")
	summary, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Uploaded != 1 || summary.Skipped != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(api.uploaded) != 2 {
		t.Fatalf("uploaded = %v", api.uploaded)
	}
}

func TestMirrorDryRunDoesNotUploadDeleteOrMutateCache(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.js")
	writeFile(t, mainPath, "v1")
	writeFile(t, filepath.Join(root, "new.js"), "v1")
	writeFile(t, filepath.Join(root, "notes.txt"), "ignored")

	info, err := os.Stat(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	state := NewState()
	state.RememberUpload("main.js", FileStampFromInfo(info))
	state.RememberUpload("old.js", FileStamp{Size: 3, ModTime: 4})

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{
		{Filename: "main.js"},
		{Filename: "old.js"},
		{Filename: "/invalid.js"},
	}}

	summary, err := Mirror(context.Background(), api, Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		State:    state,
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Uploaded != 1 || summary.Skipped != 1 || summary.Deleted != 1 || summary.Ignored != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(api.uploaded) != 0 {
		t.Fatalf("uploaded = %v", api.uploaded)
	}
	if len(api.deleted) != 0 {
		t.Fatalf("deleted = %v", api.deleted)
	}
	if _, ok := state.UploadCache["main.js"]; !ok {
		t.Fatal("main.js cache entry missing")
	}
	if _, ok := state.UploadCache["old.js"]; !ok {
		t.Fatal("old.js cache entry was mutated")
	}
}

func TestMirrorClearsUploadCacheOnDelete(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.js"), "ok")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{{Filename: "main.js"}, {Filename: "stale.js"}}}
	state := NewState()
	options := Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		State:    state,
	}
	if _, err := Mirror(context.Background(), api, options); err != nil {
		t.Fatal(err)
	}
	if _, ok := state.UploadCache["main.js"]; !ok {
		t.Fatal("expected uploaded file in cache")
	}
	if _, ok := state.UploadCache["stale.js"]; ok {
		t.Fatal("stale remote path should not be cached")
	}
}

type fakeAPI struct {
	remoteMetadata []FileMetadata
	uploaded       []string
	deleted        []string
}

func (api *fakeAPI) GetAllFileMetadata(_ context.Context, _ string) ([]FileMetadata, error) {
	return append([]FileMetadata{}, api.remoteMetadata...), nil
}

func (api *fakeAPI) PushFile(_ context.Context, _, filename, _ string) error {
	api.uploaded = append(api.uploaded, filename)
	return nil
}

func (api *fakeAPI) DeleteFile(_ context.Context, _, filename string) error {
	api.deleted = append(api.deleted, filename)
	return nil
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

func remotePaths(files []DesiredFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Remote)
	}
	return paths
}
