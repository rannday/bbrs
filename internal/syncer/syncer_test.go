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

func mustIgnored(t *testing.T, extra []string) IgnoredPatterns {
	t.Helper()
	ignored, err := NewIgnoredPatterns(extra)
	if err != nil {
		t.Fatal(err)
	}
	return ignored
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

func TestPathNormalizationTrimsTrailingSlashes(t *testing.T) {
	got, err := NormalizeRemotePath("scripts/batch/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "scripts/batch" {
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

func TestDefaultIgnorePatternsSkipDirectoriesCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "TARGET", "main.js"), "ignored")
	writeFile(t, filepath.Join(root, "Node_Modules", "dep.js"), "ignored")
	writeFile(t, filepath.Join(root, "src", "main.js"), "ok")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	desired, _, err := BuildDesired(root, "", patterns, mustIgnored(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	got := remotePaths(desired)
	want := []string{"src/main.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtraIgnorePatternsWork(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "vendor", "dep.js"), "ignored")
	writeFile(t, filepath.Join(root, "src", "main.map"), "ignored")
	writeFile(t, filepath.Join(root, "main.js"), "ok")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	desired, _, err := BuildDesired(root, "", patterns, mustIgnored(t, []string{"vendor,*.map"}))
	if err != nil {
		t.Fatal(err)
	}
	got := remotePaths(desired)
	want := []string{"main.js"}
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
	desired, _, err := BuildDesired(root, "", patterns, mustIgnored(t, nil))
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
	plan, err := BuildPlan(root, "scripts", patterns, mustIgnored(t, nil), nil)
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
	result, err := Mirror(context.Background(), api, Options{
		Source:      root,
		Destination: "scripts",
		Host:        "home",
		Patterns:    patterns,
		Ignored:     mustIgnored(t, nil),
		State:       NewState(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Uploaded != 1 || result.Summary.Skipped != 0 || result.Summary.Deleted != 1 || result.Summary.Ignored != 1 {
		t.Fatalf("summary = %+v", result.Summary)
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
		Ignored:  mustIgnored(t, nil),
		State:    state,
	}

	first, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary.Uploaded != 1 || first.Summary.Skipped != 0 {
		t.Fatalf("first summary = %+v", first.Summary)
	}

	second, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Summary.Uploaded != 0 || second.Summary.Skipped != 1 {
		t.Fatalf("second summary = %+v", second.Summary)
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
		Ignored:  mustIgnored(t, nil),
		State:    state,
	}

	first, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary.Uploaded != 1 || first.Summary.Skipped != 0 {
		t.Fatalf("first summary = %+v", first.Summary)
	}

	api.remoteMetadata = nil
	second, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Summary.Uploaded != 1 || second.Summary.Skipped != 0 {
		t.Fatalf("second summary = %+v", second.Summary)
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
		Ignored:  mustIgnored(t, nil),
		State:    NewState(),
	}
	if _, err := Mirror(context.Background(), api, options); err != nil {
		t.Fatal(err)
	}

	writeFile(t, path, "v2")
	result, err := Mirror(context.Background(), api, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Uploaded != 1 || result.Summary.Skipped != 0 {
		t.Fatalf("summary = %+v", result.Summary)
	}
	if len(api.uploaded) != 2 {
		t.Fatalf("uploaded = %v", api.uploaded)
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
		Ignored:  mustIgnored(t, nil),
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

func TestMirrorContinuesOnIndividualFileErrors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "good.js"), "ok")
	writeFile(t, filepath.Join(root, "bad.js"), "bad")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{
		remoteMetadata: nil,
		pushErrFor:     map[string]error{"bad.js": os.ErrPermission},
	}
	result, err := Mirror(context.Background(), api, Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		Ignored:  mustIgnored(t, nil),
		State:    NewState(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Uploaded != 1 || result.Summary.Failed != 1 {
		t.Fatalf("summary = %+v", result.Summary)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("errors = %v", result.Errors)
	}
}

func TestSyncChangesUploadsModifiedFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.js"), "v2")

	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{{Filename: "main.js"}}}
	result, err := SyncChanges(context.Background(), api, Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		Ignored:  mustIgnored(t, nil),
		State:    NewState(),
	}, ChangeSet{Modified: []string{"main.js"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Uploaded != 1 {
		t.Fatalf("summary = %+v", result.Summary)
	}
}

func TestSyncChangesDeletesRemovedFile(t *testing.T) {
	root := t.TempDir()
	patterns, err := NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{remoteMetadata: []FileMetadata{{Filename: "old.js"}}}
	result, err := SyncChanges(context.Background(), api, Options{
		Source:   root,
		Host:     "home",
		Patterns: patterns,
		Ignored:  mustIgnored(t, nil),
		State:    NewState(),
	}, ChangeSet{Deleted: []string{"old.js"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Deleted != 1 {
		t.Fatalf("summary = %+v", result.Summary)
	}
}

type fakeAPI struct {
	remoteMetadata []FileMetadata
	uploaded       []string
	deleted        []string
	pushErrFor     map[string]error
}

func (api *fakeAPI) GetAllFileMetadata(_ context.Context, _ string) ([]FileMetadata, error) {
	return append([]FileMetadata{}, api.remoteMetadata...), nil
}

func (api *fakeAPI) PushFile(_ context.Context, _, filename, _ string) error {
	if api.pushErrFor != nil {
		if err := api.pushErrFor[filename]; err != nil {
			return err
		}
	}
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
