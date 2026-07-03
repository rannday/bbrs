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
	api := &fakeAPI{remoteNames: []string{
		"scripts/main.js",
		"scripts/old.js",
		"outside/old.js",
		"scripts/data.json",
		"scripts/types.d.ts",
	}}
	summary, err := Mirror(context.Background(), api, Options{
		Source:      root,
		Destination: "scripts",
		Host:        "home",
		Patterns:    patterns,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Uploaded != 1 || summary.Deleted != 1 || summary.Ignored != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if !reflect.DeepEqual(api.deleted, []string{"scripts/old.js"}) {
		t.Fatalf("deleted = %v", api.deleted)
	}
	if !reflect.DeepEqual(api.uploaded, []string{"scripts/main.js"}) {
		t.Fatalf("uploaded = %v", api.uploaded)
	}
}

type fakeAPI struct {
	remoteNames []string
	uploaded    []string
	deleted     []string
}

func (api *fakeAPI) GetFileNames(_ context.Context, _ string) ([]string, error) {
	return append([]string{}, api.remoteNames...), nil
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
