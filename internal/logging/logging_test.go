package logging

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveLogPathExplicitDirectory(t *testing.T) {
	dir := t.TempDir()

	got, err := ResolveLogPath(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("dir = %q, want %q", filepath.Dir(got), dir)
	}
	if !strings.HasPrefix(filepath.Base(got), "bbrs_log_") {
		t.Fatalf("basename = %q", filepath.Base(got))
	}
	if !strings.HasSuffix(filepath.Base(got), ".log") {
		t.Fatalf("basename = %q", filepath.Base(got))
	}
}

func TestResolveLogPathCreatesMissingDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "logs")

	got, err := ResolveLogPath(dir, parent)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("dir = %q, want %q", filepath.Dir(got), dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected created directory: %v", err)
	}
}

func TestResolveLogPathRejectsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir.log")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveLogPath(file, dir)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveLogPathDefaultsToSourceDotBbrs(t *testing.T) {
	source := t.TempDir()

	got, err := ResolveLogPath("", source)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(source, ".bbrs")
	if filepath.Dir(got) != wantDir {
		t.Fatalf("dir = %q, want %q", filepath.Dir(got), wantDir)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("expected .bbrs directory: %v", err)
	}
}

func TestResolveLogPathUsesUnixSystemDirWhenPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix system log dir is not used on windows")
	}

	source := t.TempDir()
	systemDir := filepath.Join(t.TempDir(), "system-log")
	if err := os.MkdirAll(systemDir, 0o755); err != nil {
		t.Fatal(err)
	}

	original := unixSystemLogDir
	unixSystemLogDir = systemDir
	t.Cleanup(func() { unixSystemLogDir = original })

	got, err := ResolveLogPath("", source)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != systemDir {
		t.Fatalf("dir = %q, want %q", filepath.Dir(got), systemDir)
	}
}