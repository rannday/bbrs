package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTOMLConfig(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `
listen = "127.0.0.1"
port = 13000
destination = "scripts"
target = "n00dles"
include = ["*.txt"]
ignore = ["vendor", "tmp,*.map"]
verbose = true
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}

	file, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	if file.Listen != "127.0.0.1" {
		t.Fatalf("listen = %q", file.Listen)
	}
	if file.Port == nil || *file.Port != 13000 {
		t.Fatalf("port = %#v", file.Port)
	}
	if file.Destination != "scripts" {
		t.Fatalf("destination = %q", file.Destination)
	}
	if file.Target != "n00dles" {
		t.Fatalf("target = %q", file.Target)
	}
	if file.Verbose == nil || !*file.Verbose {
		t.Fatalf("verbose = %#v", file.Verbose)
	}
	if len(file.Include) != 1 || file.Include[0] != "*.txt" {
		t.Fatalf("include = %#v", file.Include)
	}
	if len(file.Ignore) != 2 || file.Ignore[0] != "vendor" || file.Ignore[1] != "tmp,*.map" {
		t.Fatalf("ignore = %#v", file.Ignore)
	}
}

func TestLoadIgnoresJSONConfig(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"port":13001}`), 0600); err != nil {
		t.Fatal(err)
	}

	file, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	if file.Port != nil {
		t.Fatalf("port = %#v", file.Port)
	}
}

func TestLoadInvalidConfigReportsPath(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("port ="), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(source)
	if err == nil {
		t.Fatal("expected invalid config error")
	}
	if !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("err = %v", err)
	}
}
