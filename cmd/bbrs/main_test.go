package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/rannday/bbrs/internal/syncer"
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

func testOptions(t *testing.T) syncer.Options {
	t.Helper()
	patterns, err := syncer.NewPatterns(nil)
	if err != nil {
		t.Fatal(err)
	}
	return syncer.Options{Source: t.TempDir(), Host: "home", Patterns: patterns, State: syncer.NewState()}
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
}

func TestParseConfigRequiresSource(t *testing.T) {
	_, err := parseConfig(nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--source is required") {
		t.Fatalf("err = %v", err)
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
		"--yes",
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

func TestConfirmDestructiveSkipsPromptWithYes(t *testing.T) {
	var output bytes.Buffer
	proceed, err := confirmDestructive(os.Stdin, &output, "home", "scripts", true)
	if err != nil {
		t.Fatal(err)
	}
	if !proceed {
		t.Fatal("expected proceed")
	}
	if strings.Contains(output.String(), "Proceed?") {
		t.Fatalf("unexpected prompt:\n%s", output.String())
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
	var output bytes.Buffer
	app := newApp(context.Background(), testOptions(t), &output)
	calls := 0
	app.sync = func(context.Context, syncer.RemoteAPI, syncer.Options) (syncer.Summary, error) {
		calls++
		return syncer.Summary{}, nil
	}

	app.triggerSync("local file change")
	app.triggerSync("local file change")

	if calls != 0 {
		t.Fatalf("sync calls = %d, want 0", calls)
	}
	if got := strings.Count(output.String(), "waiting for Bitburner to connect..."); got != 1 {
		t.Fatalf("waiting log count = %d, output:\n%s", got, output.String())
	}
	if strings.Contains(output.String(), "Bitburner disconnected; sync pending") {
		t.Fatalf("unexpected disconnected log before first connection:\n%s", output.String())
	}
}

func TestDisconnectedAfterPriorConnectionMarksPendingWithoutRPC(t *testing.T) {
	var output bytes.Buffer
	app := newApp(context.Background(), testOptions(t), &output)
	calls := 0
	app.sync = func(context.Context, syncer.RemoteAPI, syncer.Options) (syncer.Summary, error) {
		calls++
		return syncer.Summary{}, nil
	}
	app.markConnected()

	app.triggerSync("local file change")
	app.triggerSync("local file change")

	if calls != 0 {
		t.Fatalf("sync calls = %d, want 0", calls)
	}
	if got := strings.Count(output.String(), "Bitburner disconnected; sync pending"); got != 1 {
		t.Fatalf("pending log count = %d, output:\n%s", got, output.String())
	}
	if strings.Contains(output.String(), "waiting for Bitburner to connect...") {
		t.Fatalf("unexpected waiting log after prior connection:\n%s", output.String())
	}
}

func TestFirstConnectionRunsFullSyncAndClearsPending(t *testing.T) {
	var output bytes.Buffer
	app := newApp(context.Background(), testOptions(t), &output)
	calls := 0
	app.sync = func(context.Context, syncer.RemoteAPI, syncer.Options) (syncer.Summary, error) {
		calls++
		return syncer.Summary{Uploaded: 2}, nil
	}

	app.triggerSync("local file change")
	client := newFakeRemoteClient(true)
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}
	app.markConnected()
	app.triggerSync("Bitburner connection")

	if calls != 1 {
		t.Fatalf("sync calls = %d, want 1", calls)
	}
	if !strings.Contains(output.String(), "sync complete: uploaded=2 skipped=0 deleted=0 ignored=0") {
		t.Fatalf("missing sync complete log:\n%s", output.String())
	}
	app.syncMu.Lock()
	defer app.syncMu.Unlock()
	if app.syncPending {
		t.Fatal("syncPending still true")
	}
	if app.disconnectedPendingLogged {
		t.Fatal("disconnectedPendingLogged still true")
	}
	if app.waitingForConnectionLogged {
		t.Fatal("waitingForConnectionLogged still true")
	}
}

func TestReconnectRunsFullSyncAndClearsPending(t *testing.T) {
	var output bytes.Buffer
	app := newApp(context.Background(), testOptions(t), &output)
	calls := 0
	app.sync = func(context.Context, syncer.RemoteAPI, syncer.Options) (syncer.Summary, error) {
		calls++
		return syncer.Summary{Uploaded: 3}, nil
	}
	app.markConnected()

	app.triggerSync("local file change")
	client := newFakeRemoteClient(true)
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}
	app.markConnected()
	app.triggerSync("Bitburner connection")

	if calls != 1 {
		t.Fatalf("sync calls = %d, want 1", calls)
	}
	if !strings.Contains(output.String(), "sync complete: uploaded=3 skipped=0 deleted=0 ignored=0") {
		t.Fatalf("missing sync complete log:\n%s", output.String())
	}
	app.syncMu.Lock()
	defer app.syncMu.Unlock()
	if app.syncPending {
		t.Fatal("syncPending still true")
	}
	if app.disconnectedPendingLogged {
		t.Fatal("disconnectedPendingLogged still true")
	}
}

func TestOverlappingSyncRequestsAreCoalesced(t *testing.T) {
	var output bytes.Buffer
	app := newApp(context.Background(), testOptions(t), &output)
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
	app.sync = func(context.Context, syncer.RemoteAPI, syncer.Options) (syncer.Summary, error) {
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
		return syncer.Summary{Uploaded: call}, nil
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
	var output bytes.Buffer
	app := newApp(context.Background(), testOptions(t), &output)
	client := newFakeRemoteClient(true)
	client.SetDisconnectHandler(func(err error) {
		app.handleClientDisconnected(client, err)
	})
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}

	client.MarkDisconnected(errors.New("write failed: use of closed network connection"))
	client.MarkDisconnected(errors.New("again"))

	if got := strings.Count(output.String(), "Bitburner connection lost:"); got != 1 {
		t.Fatalf("connection lost log count = %d, output:\n%s", got, output.String())
	}
	if app.activeClient() != nil {
		t.Fatal("active client not cleared")
	}
}

func TestRunOneSyncRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var output bytes.Buffer
	app := newApp(ctx, testOptions(t), &output)
	client := newFakeRemoteClient(true)
	if !app.setClient(client) {
		t.Fatal("setClient failed")
	}
	app.sync = func(ctx context.Context, _ syncer.RemoteAPI, _ syncer.Options) (syncer.Summary, error) {
		return syncer.Summary{}, ctx.Err()
	}

	if app.runOneSync("cancelled") {
		t.Fatal("expected cancelled sync to fail")
	}
}