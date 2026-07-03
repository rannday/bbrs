package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigDefaultValues(t *testing.T) {
	source := t.TempDir()
	cfg, err := parseConfig([]string{"-s", source}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != filepath.Clean(source) {
		t.Fatalf("source = %q, want %q", cfg.Source, filepath.Clean(source))
	}
	if cfg.Listen != "127.0.0.1" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.Port != 12525 {
		t.Fatalf("port = %d", cfg.Port)
	}
	if cfg.Host != "home" {
		t.Fatalf("host = %q", cfg.Host)
	}
	if cfg.Destination != "" {
		t.Fatalf("destination = %q", cfg.Destination)
	}
}

func TestParseConfigRequiresSource(t *testing.T) {
	_, err := parseConfig(nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--source is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestHelpIncludesPatternExamples(t *testing.T) {
	var output bytes.Buffer
	_, err := parseConfig([]string{"--help"}, &output)
	if err != flag.ErrHelp {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	text := output.String()
	for _, want := range []string{
		"Pattern examples:",
		"--pattern '*.txt'",
		"--pattern '*.js,*.ts,*.ns'",
		"--pattern '*.script' --pattern '*.txt'",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q:\n%s", want, text)
		}
	}
}

func TestParseConfigRejectsNonDirectorySource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := parseConfig([]string{"--source", file}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("err = %v", err)
	}
}
