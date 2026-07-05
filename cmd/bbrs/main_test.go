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
	payload := `{"port":13010,"destination":"scripts","ignore":["dist","tmp,*.map"],"verbose":true}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(payload), 0600); err != nil {
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
	if !cfg.Verbose {
		t.Fatal("verbose not set from config")
	}
	if len(cfg.Ignore) != 2 || cfg.Ignore[0] != "dist" || cfg.Ignore[1] != "tmp,*.map" {
		t.Fatalf("ignore = %#v", cfg.Ignore)
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
		"-d, --destination          Destination directory inside Bitburner. Default: root.",
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
		"--host",
		"--pattern",
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
