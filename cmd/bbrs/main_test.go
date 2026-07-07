package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rannday/bbrs/internal/syncer"
	logx "github.com/rannday/go-log"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "bbrs-test-config-")
	if err != nil {
		panic(err)
	}
	defaultConfigPaths = configPaths{
		SystemEnvPath: filepath.Join(root, "missing-system.env"),
		UserEnvPath:   filepath.Join(root, "missing-user.env"),
	}
	restoreEnv := unsetBBRSEnv()

	code := m.Run()

	restoreEnv()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func unsetBBRSEnv() func() {
	type previous struct {
		key   string
		value string
		ok    bool
	}
	values := make([]previous, 0, len(bbrsEnvKeys))
	for _, key := range bbrsEnvKeys {
		value, ok := os.LookupEnv(key)
		values = append(values, previous{key: key, value: value, ok: ok})
		_ = os.Unsetenv(key)
	}
	return func() {
		for _, prev := range values {
			if prev.ok {
				_ = os.Setenv(prev.key, prev.value)
				continue
			}
			_ = os.Unsetenv(prev.key)
		}
	}
}

type fakeRemoteClient struct {
	mu           sync.Mutex
	connected    bool
	disconnected chan struct{}
	handler      func(error)
	closed       bool
}

func newFakeRemoteClient(connected bool) *fakeRemoteClient {
	client := &fakeRemoteClient{
		connected:    connected,
		disconnected: make(chan struct{}),
	}
	if !connected {
		close(client.disconnected)
		client.closed = true
	}
	return client
}

func (client *fakeRemoteClient) Connected() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.connected
}

func (client *fakeRemoteClient) Disconnected() <-chan struct{} {
	return client.disconnected
}

func (client *fakeRemoteClient) Close(websocket.StatusCode, string) error {
	return nil
}

func (client *fakeRemoteClient) SetDisconnectHandler(handler func(error)) {
	client.mu.Lock()
	client.handler = handler
	client.mu.Unlock()
}

func (client *fakeRemoteClient) MarkDisconnected(err error) bool {
	client.mu.Lock()
	if !client.connected {
		client.mu.Unlock()
		return false
	}
	client.connected = false
	if !client.closed {
		close(client.disconnected)
		client.closed = true
	}
	handler := client.handler
	client.mu.Unlock()
	if handler != nil {
		handler(err)
	}
	return true
}

func (client *fakeRemoteClient) GetAllFileMetadata(context.Context, string) ([]syncer.FileMetadata, error) {
	return nil, errors.New("unexpected GetAllFileMetadata")
}

func (client *fakeRemoteClient) PushFile(context.Context, string, string, string) error {
	return errors.New("unexpected PushFile")
}

func (client *fakeRemoteClient) DeleteFile(context.Context, string, string) error {
	return errors.New("unexpected DeleteFile")
}

func setupTestLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logx.SetLogger(slog.New(handler))
	t.Cleanup(logx.Reset)
	return buf
}

func testOptions(t *testing.T) syncer.Options {
	t.Helper()
	patterns, err := syncer.NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	ignored, err := syncer.NewIgnoredPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	return syncer.Options{
		Source:   t.TempDir(),
		Host:     "home",
		Patterns: patterns,
		Ignored:  ignored,
		State:    syncer.NewState(),
	}
}

func testConfigPaths(t *testing.T) configPaths {
	t.Helper()
	root := t.TempDir()
	return configPaths{
		SystemEnvPath: filepath.Join(root, "system.env"),
		UserEnvPath:   filepath.Join(root, "user.env"),
	}
}

func writeEnvFile(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeSourceConfig(t *testing.T, source, payload string) {
	t.Helper()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
}

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
	if cfg.Target != "home" {
		t.Fatalf("target = %q", cfg.Target)
	}
	if cfg.Destination != "bbrs" {
		t.Fatalf("destination = %q", cfg.Destination)
	}
	if cfg.LogDir != "" {
		t.Fatalf("logdir = %q", cfg.LogDir)
	}
	if cfg.Verbose {
		t.Fatal("verbose enabled by default")
	}
}

func TestRunVersionFlagsPrintWithoutSource(t *testing.T) {
	var outputs []string
	for _, flag := range []string{"-v", "--version"} {
		var output bytes.Buffer
		if err := run([]string{flag}, os.Stdin, &output, &bytes.Buffer{}); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if !strings.HasSuffix(output.String(), "\n") {
			t.Fatalf("%s version output = %q", flag, output.String())
		}
		outputs = append(outputs, output.String())
	}
	if outputs[0] != outputs[1] {
		t.Fatalf("-v output = %q, --version output = %q", outputs[0], outputs[1])
	}
}

func TestParseConfigRequiresSource(t *testing.T) {
	_, err := parseConfig(nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--source is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseConfigAllowsExplicitListenAddress(t *testing.T) {
	source := t.TempDir()
	cfg, err := parseConfig([]string{"-s", source, "--listen", "0.0.0.0"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
}

func TestParseConfigLoadsFileDefaults(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `
port = 13010
destination = "scripts"
target = "n00dles"
include = ["*.txt", "*.ns"]
ignore = ["dist", "tmp,*.map"]
verbose = true
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig([]string{"-s", source}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 13010 {
		t.Fatalf("port = %d", cfg.Port)
	}
	if cfg.Destination != "scripts" {
		t.Fatalf("destination = %q", cfg.Destination)
	}
	if cfg.Target != "n00dles" {
		t.Fatalf("target = %q", cfg.Target)
	}
	if !cfg.Verbose {
		t.Fatal("verbose not set from config")
	}
	if len(cfg.Include) != 2 || cfg.Include[0] != "*.txt" || cfg.Include[1] != "*.ns" {
		t.Fatalf("include = %#v", cfg.Include)
	}
	if len(cfg.Ignore) != 2 || cfg.Ignore[0] != "dist" || cfg.Ignore[1] != "tmp,*.map" {
		t.Fatalf("ignore = %#v", cfg.Ignore)
	}
}

func TestParseConfigSystemEnvOverridesDefaults(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, `
BBRS_LISTEN=0.0.0.0
BBRS_PORT=13001
BBRS_DESTINATION=scripts
BBRS_TARGET=n00dles
BBRS_INCLUDE=*.txt,*.ns
BBRS_IGNORE=vendor,tmp
BBRS_LOG_DIR=system-log
BBRS_VERBOSE=true
`)

	cfg, err := parseConfigWithPaths([]string{"-s", source}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0" || cfg.Port != 13001 || cfg.Destination != "scripts" || cfg.Target != "n00dles" {
		t.Fatalf("cfg = %#v", cfg)
	}
	if cfg.LogDir != "system-log" || !cfg.Verbose {
		t.Fatalf("log/verbose = %q/%v", cfg.LogDir, cfg.Verbose)
	}
	if len(cfg.Include) != 2 || cfg.Include[0] != "*.txt" || cfg.Include[1] != "*.ns" {
		t.Fatalf("include = %#v", cfg.Include)
	}
	if len(cfg.Ignore) != 2 || cfg.Ignore[0] != "vendor" || cfg.Ignore[1] != "tmp" {
		t.Fatalf("ignore = %#v", cfg.Ignore)
	}
}

func TestParseConfigUserEnvOverridesSystemEnv(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, "BBRS_PORT=13001\nBBRS_TARGET=system\nBBRS_INCLUDE=system\n")
	writeEnvFile(t, paths.UserEnvPath, "BBRS_PORT=13002\nBBRS_TARGET=user\nBBRS_INCLUDE=user-a,user-b\n")

	cfg, err := parseConfigWithPaths([]string{"-s", source}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 13002 || cfg.Target != "user" {
		t.Fatalf("cfg = %#v", cfg)
	}
	if len(cfg.Include) != 2 || cfg.Include[0] != "user-a" || cfg.Include[1] != "user-b" {
		t.Fatalf("include = %#v", cfg.Include)
	}
}

func TestParseConfigProjectConfigOverridesEnvFiles(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, "BBRS_PORT=13001\nBBRS_TARGET=system\n")
	writeEnvFile(t, paths.UserEnvPath, "BBRS_PORT=13002\nBBRS_TARGET=user\n")
	writeSourceConfig(t, source, `
port = 13003
target = "project"
include = ["project"]
ignore = ["project-ignore"]
`)

	cfg, err := parseConfigWithPaths([]string{"-s", source}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 13003 || cfg.Target != "project" {
		t.Fatalf("cfg = %#v", cfg)
	}
	if len(cfg.Include) != 1 || cfg.Include[0] != "project" {
		t.Fatalf("include = %#v", cfg.Include)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "project-ignore" {
		t.Fatalf("ignore = %#v", cfg.Ignore)
	}
}

func TestParseConfigProcessEnvOverridesProjectConfig(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, "BBRS_PORT=13001\n")
	writeSourceConfig(t, source, `
port = 13003
target = "project"
include = ["project"]
`)
	t.Setenv("BBRS_PORT", "13004")
	t.Setenv("BBRS_TARGET", "process")
	t.Setenv("BBRS_INCLUDE", "process-a, process-b")

	cfg, err := parseConfigWithPaths([]string{"-s", source}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 13004 || cfg.Target != "process" {
		t.Fatalf("cfg = %#v", cfg)
	}
	if len(cfg.Include) != 2 || cfg.Include[0] != "process-a" || cfg.Include[1] != "process-b" {
		t.Fatalf("include = %#v", cfg.Include)
	}
}

func TestParseConfigCLIOverridesEverything(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, "BBRS_PORT=13001\nBBRS_TARGET=system\n")
	writeEnvFile(t, paths.UserEnvPath, "BBRS_PORT=13002\nBBRS_TARGET=user\n")
	writeSourceConfig(t, source, "port = 13003\ntarget = \"project\"\nverbose = true\n")
	t.Setenv("BBRS_PORT", "13004")
	t.Setenv("BBRS_TARGET", "process")
	t.Setenv("BBRS_VERBOSE", "true")

	cfg, err := parseConfigWithPaths([]string{
		"-s", source,
		"--port", "13005",
		"--target", "cli",
		"--verbose=false",
	}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 13005 || cfg.Target != "cli" || cfg.Verbose {
		t.Fatalf("cfg = %#v", cfg)
	}
}

func TestParseConfigProcessEnvSourceLoadsProjectConfigFromThatSource(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeSourceConfig(t, source, "port = 13007\n")
	t.Setenv("BBRS_SOURCE", source)

	cfg, err := parseConfigWithPaths(nil, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != filepath.Clean(source) || cfg.Port != 13007 {
		t.Fatalf("cfg = %#v", cfg)
	}
}

func TestParseConfigCLIListFlagsReplaceEarlierListLayers(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, "BBRS_INCLUDE=system\nBBRS_IGNORE=system-ignore\n")
	writeSourceConfig(t, source, `
include = ["project"]
ignore = ["project-ignore"]
`)
	t.Setenv("BBRS_INCLUDE", "process")
	t.Setenv("BBRS_IGNORE", "process-ignore")

	cfg, err := parseConfigWithPaths([]string{
		"-s", source,
		"--include", "cli-a,cli-b",
		"--include", "cli-c",
		"--ignore", "cli-ignore",
	}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Include) != 2 || cfg.Include[0] != "cli-a,cli-b" || cfg.Include[1] != "cli-c" {
		t.Fatalf("include = %#v", cfg.Include)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "cli-ignore" {
		t.Fatalf("ignore = %#v", cfg.Ignore)
	}

	patterns, err := syncer.NewPatterns(cfg.Include)
	if err != nil {
		t.Fatal(err)
	}
	got := patterns.IncludePatterns()
	if len(got) != 5 || got[0] != "*.js" || got[1] != "*.ts" || got[2] != "cli-a" || got[3] != "cli-b" || got[4] != "cli-c" {
		t.Fatalf("expanded include = %#v", got)
	}
}

func TestParseConfigMissingEnvFilesIgnored(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)

	cfg, err := parseConfigWithPaths([]string{"-s", source}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 12525 {
		t.Fatalf("port = %d", cfg.Port)
	}
}

func TestParseConfigMalformedEnvFileFailsClearly(t *testing.T) {
	source := t.TempDir()
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, "BBRS_PORT\n")

	_, err := parseConfigWithPaths([]string{"-s", source}, &bytes.Buffer{}, paths)
	if err == nil {
		t.Fatal("expected malformed env file error")
	}
	text := err.Error()
	if !strings.Contains(text, "load env file") || !strings.Contains(text, paths.SystemEnvPath) || !strings.Contains(text, "dotenv") || !strings.Contains(text, ":1:") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseConfigVersionAndHelpSkipEnvFileLoading(t *testing.T) {
	paths := testConfigPaths(t)
	writeEnvFile(t, paths.SystemEnvPath, "BBRS_PORT\n")

	cfg, err := parseConfigWithPaths([]string{"--version"}, &bytes.Buffer{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Version {
		t.Fatal("version not set")
	}

	var output bytes.Buffer
	_, err = parseConfigWithPaths([]string{"--help"}, &output, paths)
	if err != flag.ErrHelp {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(output.String(), "Configuration precedence:") {
		t.Fatalf("help missing precedence:\n%s", output.String())
	}
}

func TestParseConfigExplicitFlagsOverrideFileConfig(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := `
listen = "0.0.0.0"
port = 13010
destination = "scripts"
target = "n00dles"
include = ["cfg.txt"]
ignore = ["cfg-ignore"]
log_dir = "cfg-log"
verbose = true
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig([]string{
		"-s", source,
		"--listen", "127.0.0.1",
		"--port", "12525",
		"--destination", "bbrs",
		"--target", "home",
		"--include", "cli.txt",
		"--include", "cli.ns",
		"--ignore", "cli-ignore",
		"--log-dir", "cli-log",
		"--verbose=false",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.Port != 12525 {
		t.Fatalf("port = %d", cfg.Port)
	}
	if cfg.Destination != "bbrs" {
		t.Fatalf("destination = %q", cfg.Destination)
	}
	if cfg.Target != "home" {
		t.Fatalf("target = %q", cfg.Target)
	}
	if len(cfg.Include) != 2 || cfg.Include[0] != "cli.txt" || cfg.Include[1] != "cli.ns" {
		t.Fatalf("include = %#v", cfg.Include)
	}
	if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "cli-ignore" {
		t.Fatalf("ignore = %#v", cfg.Ignore)
	}
	if cfg.LogDir != "cli-log" {
		t.Fatalf("logdir = %q", cfg.LogDir)
	}
	if cfg.Verbose {
		t.Fatal("verbose config value overrode explicit CLI false")
	}
}

func TestParseConfigVerboseFlagOverridesFileFalse(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("verbose = false\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig([]string{"-s", source, "--verbose"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Verbose {
		t.Fatal("verbose flag did not override config false")
	}
}

func TestParseConfigAcceptsTargetFlags(t *testing.T) {
	source := t.TempDir()
	for _, args := range [][]string{
		{"-s", source, "-t", "n00dles"},
		{"-s", source, "--target", "n00dles"},
	} {
		cfg, err := parseConfig(args, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Target != "n00dles" {
			t.Fatalf("%v target = %q", args, cfg.Target)
		}
	}
}

func TestParseConfigAcceptsRepeatedAndCommaSeparatedInclude(t *testing.T) {
	source := t.TempDir()
	cfg, err := parseConfig([]string{"-s", source, "--include", "*.txt,*.ns", "--include", "*.script"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Include) != 2 || cfg.Include[0] != "*.txt,*.ns" || cfg.Include[1] != "*.script" {
		t.Fatalf("include = %#v", cfg.Include)
	}
}

func TestParseConfigAcceptsRepeatedAndCommaSeparatedIgnore(t *testing.T) {
	source := t.TempDir()
	cfg, err := parseConfig([]string{"-s", source, "--ignore", "dist,tmp", "--ignore", "*.map"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Ignore) != 2 || cfg.Ignore[0] != "dist,tmp" || cfg.Ignore[1] != "*.map" {
		t.Fatalf("ignore = %#v", cfg.Ignore)
	}
}

func TestParseConfigNormalizesDestination(t *testing.T) {
	source := t.TempDir()
	cfg, err := parseConfig([]string{"-s", source, "-d", "scripts/batch/"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Destination != "scripts/batch" {
		t.Fatalf("destination = %q", cfg.Destination)
	}
}

func TestParseConfigAcceptsLogDir(t *testing.T) {
	source := t.TempDir()
	logDir := filepath.Join(source, "logs")
	cfg, err := parseConfig([]string{"-s", source, "--log-dir", logDir}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogDir != logDir {
		t.Fatalf("logdir = %q, want %q", cfg.LogDir, logDir)
	}
}

func TestParseConfigRejectsUnsafeDestination(t *testing.T) {
	source := t.TempDir()
	_, err := parseConfig([]string{"-s", source, "-d", "../escape"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "invalid destination") {
		t.Fatalf("err = %v", err)
	}
}

func TestHelpIncludesIncludeExamples(t *testing.T) {
	var output bytes.Buffer
	_, err := parseConfig([]string{"--help"}, &output)
	if err != flag.ErrHelp {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	text := output.String()
	for _, want := range []string{
		"Include examples:",
		"--include '*.txt'",
		"--include '*.js,*.ts,*.ns'",
		"--include '*.script' --include '*.txt'",
		"Ignore examples:",
		"--ignore dist",
		"--ignore dist,tmp,*.map",
		"--ignore vendor --ignore '*.map'",
		"Logging:",
		"Default: /var/log/bbrs/",
		"Persistent cache:",
		"Config file:",
		"--verbose",
		"--log-dir              Directory for log files.",
		"-d, --destination          Destination directory inside Bitburner. Default: /bbrs/",
		"-t, --target               Target Bitburner host. Default: home",
		"-v, --version              Print version and exit.",
		"-h, --help                 Show help.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q:\n%s", want, text)
		}
	}
	for _, removed := range []string{
		"--dry-run",
		"--once",
		"-y, --yes",
		"--allow-remote-listen",
		"--ignore-dir",
		"--pattern",
		"--host",
		"config.json",
		"Default: root",
		"Default: empty/root",
	} {
		if strings.Contains(text, removed) {
			t.Fatalf("help contains removed text %q:\n%s", removed, text)
		}
	}
	assertHelpOrder(t, text, []string{
		"-s, --source",
		"-d, --destination",
		"-l, --listen",
		"-p, --port",
		"-t, --target",
		"--include",
		"--ignore",
		"--log-dir",
		"--verbose",
		"-v, --version",
		"-h, --help",
	})
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

func TestNoActivePingLoop(t *testing.T) {
	for _, file := range []string{"main.go", filepath.Join("..", "..", "internal", "bitburner", "client.go")} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, ".Ping(") || strings.Contains(text, "failed to ping") {
			t.Fatalf("%s still contains active ping handling", file)
		}
	}
}

func TestStartupFileChangeWaitsForFirstConnectionWithoutRPC(t *testing.T) {
	output := setupTestLogger(t)
	app := newApp(context.Background(), testOptions(t))
	calls := 0
	app.syncFn = func(context.Context, syncer.RemoteAPI, syncer.Options, syncer.ChangeSet) (syncer.Result, error) {
		calls++
		return syncer.Result{}, nil
	}

	app.triggerSync("local file change")
	app.triggerSync("local file change")
	waitForSyncWorker(t, app)

	if calls != 0 {
		t.Fatalf("sync calls = %d, want 0", calls)
	}
	if got := strings.Count(output.String(), "waiting for Bitburner to connect"); got != 1 {
		t.Fatalf("waiting log count = %d, output:\n%s", got, output.String())
	}
	if strings.Contains(output.String(), "Bitburner disconnected; sync pending") {
		t.Fatalf("unexpected disconnected log before first connection:\n%s", output.String())
	}
}

func TestDisconnectedAfterPriorConnectionMarksPendingWithoutRPC(t *testing.T) {
	output := setupTestLogger(t)
	app := newApp(context.Background(), testOptions(t))
	calls := 0
	app.syncFn = func(context.Context, syncer.RemoteAPI, syncer.Options, syncer.ChangeSet) (syncer.Result, error) {
		calls++
		return syncer.Result{}, nil
	}
	app.markConnected()

	app.triggerSync("local file change")
	app.triggerSync("local file change")
	waitForSyncWorker(t, app)

	if calls != 0 {
		t.Fatalf("sync calls = %d, want 0", calls)
	}
	if got := strings.Count(output.String(), "Bitburner disconnected; sync pending"); got != 1 {
		t.Fatalf("pending log count = %d, output:\n%s", got, output.String())
	}
	if strings.Contains(output.String(), "waiting for Bitburner to connect") {
		t.Fatalf("unexpected waiting log after prior connection:\n%s", output.String())
	}
}

func TestFirstConnectionRunsFullSyncAndClearsPending(t *testing.T) {
	output := setupTestLogger(t)
	app := newApp(context.Background(), testOptions(t))
	calls := 0
	app.syncFn = func(context.Context, syncer.RemoteAPI, syncer.Options, syncer.ChangeSet) (syncer.Result, error) {
		calls++
		return syncer.Result{Summary: syncer.Summary{Uploaded: 2}}, nil
	}

	app.triggerSync("local file change")
	client := newFakeRemoteClient(true)
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}
	app.markConnected()
	app.triggerSync("Bitburner connection")
	waitForSyncWorker(t, app)

	if calls != 1 {
		t.Fatalf("sync calls = %d, want 1", calls)
	}
	text := output.String()
	if !strings.Contains(text, "sync complete") || !strings.Contains(text, "uploaded=2") {
		t.Fatalf("missing sync complete log:\n%s", text)
	}
	app.workerMu.Lock()
	defer app.workerMu.Unlock()
	if app.pending != nil {
		t.Fatal("pending job still set")
	}
	if app.disconnectedPendingLogged {
		t.Fatal("disconnectedPendingLogged still true")
	}
	if app.waitingForConnectionLogged {
		t.Fatal("waitingForConnectionLogged still true")
	}
}

func TestReconnectRunsFullSyncAndClearsPending(t *testing.T) {
	output := setupTestLogger(t)
	app := newApp(context.Background(), testOptions(t))
	calls := 0
	app.syncFn = func(context.Context, syncer.RemoteAPI, syncer.Options, syncer.ChangeSet) (syncer.Result, error) {
		calls++
		return syncer.Result{Summary: syncer.Summary{Uploaded: 3}}, nil
	}
	app.markConnected()

	app.triggerSync("local file change")
	client := newFakeRemoteClient(true)
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}
	app.markConnected()
	app.triggerSync("Bitburner connection")
	waitForSyncWorker(t, app)

	if calls != 1 {
		t.Fatalf("sync calls = %d, want 1", calls)
	}
	text := output.String()
	if !strings.Contains(text, "sync complete") || !strings.Contains(text, "uploaded=3") {
		t.Fatalf("missing sync complete log:\n%s", text)
	}
	app.workerMu.Lock()
	defer app.workerMu.Unlock()
	if app.pending != nil {
		t.Fatal("pending job still set")
	}
	if app.disconnectedPendingLogged {
		t.Fatal("disconnectedPendingLogged still true")
	}
}

func TestOverlappingSyncRequestsAreCoalesced(t *testing.T) {
	setupTestLogger(t)
	app := newApp(context.Background(), testOptions(t))
	client := newFakeRemoteClient(true)
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}

	started := make(chan int, 2)
	release := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	running := 0
	maxRunning := 0
	app.syncFn = func(context.Context, syncer.RemoteAPI, syncer.Options, syncer.ChangeSet) (syncer.Result, error) {
		mu.Lock()
		calls++
		call := calls
		running++
		if running > maxRunning {
			maxRunning = running
		}
		mu.Unlock()

		started <- call
		if call == 1 {
			<-release
		}

		mu.Lock()
		running--
		mu.Unlock()
		return syncer.Result{Summary: syncer.Summary{Uploaded: call}}, nil
	}

	go func() {
		app.triggerSync("first")
		close(done)
	}()
	if call := <-started; call != 1 {
		t.Fatalf("first call = %d", call)
	}

	app.triggerSync("second")
	close(release)
	if call := <-started; call != 2 {
		t.Fatalf("second call = %d", call)
	}
	<-done
	waitForSyncWorker(t, app)

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if maxRunning != 1 {
		t.Fatalf("maxRunning = %d, want 1", maxRunning)
	}
}

func TestConnectionLostLoggingOncePerDisconnect(t *testing.T) {
	output := setupTestLogger(t)
	app := newApp(context.Background(), testOptions(t))
	client := newFakeRemoteClient(true)
	client.SetDisconnectHandler(func(err error) {
		app.handleClientDisconnected(client, err)
	})
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}

	client.MarkDisconnected(errors.New("write failed: use of closed network connection"))
	client.MarkDisconnected(errors.New("again"))

	if got := strings.Count(output.String(), "Bitburner connection lost"); got != 1 {
		t.Fatalf("connection lost log count = %d, output:\n%s", got, output.String())
	}
	if app.activeClient() != nil {
		t.Fatal("active client not cleared")
	}
}

func TestRunOneSyncRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	setupTestLogger(t)
	app := newApp(ctx, testOptions(t))
	client := newFakeRemoteClient(true)
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}
	app.syncFn = func(ctx context.Context, _ syncer.RemoteAPI, _ syncer.Options, _ syncer.ChangeSet) (syncer.Result, error) {
		return syncer.Result{}, ctx.Err()
	}

	if app.runOneSync("cancelled", syncer.ChangeSet{}) {
		t.Fatal("expected cancelled sync to fail")
	}
}

func assertHelpOrder(t *testing.T, text string, wants []string) {
	t.Helper()
	last := -1
	for _, want := range wants {
		idx := strings.Index(text, want)
		if idx == -1 {
			t.Fatalf("help missing %q:\n%s", want, text)
		}
		if idx < last {
			t.Fatalf("help option %q out of order:\n%s", want, text)
		}
		last = idx
	}
}

func waitForSyncWorker(t *testing.T, app *app) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.workerMu.Lock()
		running := app.running
		app.workerMu.Unlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("sync worker still running")
}
