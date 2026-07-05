package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadJSONConfig(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `{
  "listen": "127.0.0.1",
  "port": 13000,
  "destination": "scripts",
  "host": "home",
  "patterns": ["*.txt"],
  "ignore": ["vendor", "tmp,*.map"],
  "verbose": true
}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(payload), 0600); err != nil {
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
	if file.Verbose == nil || !*file.Verbose {
		t.Fatalf("verbose = %#v", file.Verbose)
	}
	if len(file.Ignore) != 2 || file.Ignore[0] != "vendor" || file.Ignore[1] != "tmp,*.map" {
		t.Fatalf("ignore = %#v", file.Ignore)
	}
}

func TestLoadTOMLConfig(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `
listen = "127.0.0.1"
port = 13001
destination = "batch"
ignore = ["dist", "*.map"]
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}

	file, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	if file.Port == nil || *file.Port != 13001 {
		t.Fatalf("port = %#v", file.Port)
	}
	if len(file.Ignore) != 2 || file.Ignore[0] != "dist" || file.Ignore[1] != "*.map" {
		t.Fatalf("ignore = %#v", file.Ignore)
	}
}
