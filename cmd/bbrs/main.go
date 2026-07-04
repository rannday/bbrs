package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	logx "github.com/rannday/go-log"
	"github.com/rannday/bbrs/internal/bitburner"
	"github.com/rannday/bbrs/internal/logging"
	"github.com/rannday/bbrs/internal/syncer"
	"github.com/rannday/bbrs/internal/watch"
)

type config struct {
	Source      string
	Listen      string
	Port        int
	Destination string
	Host        string
	Patterns    []string
	Yes         bool
	LogDir      string
}

type patternFlags []string

func (flags *patternFlags) String() string {
	return strings.Join(*flags, ",")
}

func (flags *patternFlags) Set(value string) error {
	*flags = append(*flags, value)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		logx.ErrorErr("bbrs failed", err)
		os.Exit(1)
	}
}

func run(args []string, stdin *os.File, stdout, stderr io.Writer) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	logPath, err := logging.ResolveLogPath(cfg.LogDir, cfg.Source)
	if err != nil {
		return err
	}
	if err := logging.Configure(logPath); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}
	logx.Info("logging enabled", "path", logPath)

	patterns, err := syncer.NewPatterns(cfg.Patterns)
	if err != nil {
		return err
	}

	proceed, err := confirmDestructive(stdin, stdout, cfg.Host, cfg.Destination, cfg.Yes)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := newApp(ctx, syncer.Options{
		Source:      cfg.Source,
		Destination: cfg.Destination,
		Host:        cfg.Host,
		Patterns:    patterns,
		State:       syncer.NewState(),
	})

	go func() {
		if err := watch.Poll(ctx, cfg.Source, patterns, 750*time.Millisecond, 200*time.Millisecond, func() {
			app.triggerSync("local file change")
		}); err != nil && !errors.Is(err, context.Canceled) {
			logx.ErrorErr("watch failed", err)
		}
	}()

	address := net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.Port))
	server := &http.Server{
		Addr:              address,
		ReadHeaderTimeout: 5 * time.Second,
		Handler:           app.handler(),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	serverErr := make(chan error, 1)
	go func() {
		logx.Info("listening for Bitburner Remote API websocket", "address", "ws://"+address)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-sigCh:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	return nil
}

func parseConfig(args []string, output io.Writer) (config, error) {
	var cfg config
	cfg.Listen = "127.0.0.1"
	cfg.Port = 12525
	cfg.Host = "home"

	fs := flag.NewFlagSet("bbrs", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprint(output, helpText())
	}

	var help bool
	var patterns patternFlags
	fs.BoolVar(&help, "h", false, "show help")
	fs.BoolVar(&help, "help", false, "show help")
	fs.BoolVar(&cfg.Yes, "y", false, "skip destructive-operation confirmation")
	fs.BoolVar(&cfg.Yes, "yes", false, "skip destructive-operation confirmation")
	fs.StringVar(&cfg.Source, "s", "", "local source directory to sync")
	fs.StringVar(&cfg.Source, "source", "", "local source directory to sync")
	fs.StringVar(&cfg.Listen, "l", cfg.Listen, "listen address")
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "listen address")
	fs.IntVar(&cfg.Port, "p", cfg.Port, "listen port")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "listen port")
	fs.StringVar(&cfg.Destination, "d", "", "destination directory inside Bitburner")
	fs.StringVar(&cfg.Destination, "destination", "", "destination directory inside Bitburner")
	fs.StringVar(&cfg.Host, "host", cfg.Host, "destination Bitburner host")
	fs.Var(&patterns, "pattern", "additional filename pattern to include")
	fs.StringVar(&cfg.LogDir, "logdir", "", "directory for log files")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if help {
		fs.Usage()
		return config{}, flag.ErrHelp
	}
	if cfg.Source == "" {
		return config{}, fmt.Errorf("--source is required")
	}
	info, err := os.Stat(cfg.Source)
	if err != nil {
		return config{}, fmt.Errorf("source %q: %w", cfg.Source, err)
	}
	if !info.IsDir() {
		return config{}, fmt.Errorf("source %q is not a directory", cfg.Source)
	}
	source, err := filepath.Abs(cfg.Source)
	if err != nil {
		return config{}, fmt.Errorf("resolve source %q: %w", cfg.Source, err)
	}
	cfg.Source = filepath.Clean(source)
	if cfg.Destination != "" {
		normalized, err := syncer.NormalizeRemotePath(cfg.Destination)
		if err != nil {
			return config{}, fmt.Errorf("invalid destination %q: %w", cfg.Destination, err)
		}
		cfg.Destination = normalized
	}
	cfg.Patterns = append([]string{}, patterns...)
	return cfg, nil
}

func helpText() string {
	return `Usage:
  bbrs -s ./source-dir [options]

Options:
  -h, --help                 Show help.
  -s, --source               Local source directory to sync. Required.
  -l, --listen               Listen address. Default: 127.0.0.1.
  -p, --port                 Listen port. Default: 12525.
  -d, --destination          Destination directory inside Bitburner. Default: empty/root.
      --host                 Destination Bitburner host. Default: home.
      --pattern              Additional filename patterns to include.
      --logdir               Directory for log files.
  -y, --yes                  Skip destructive-operation confirmation.

Pattern examples:
  --pattern '*.txt'
  --pattern '*.js,*.ts,*.ns'
  --pattern '*.script' --pattern '*.txt'

Logging:
  Default: /var/log/bbrs/ on Unix when present, otherwise <source>/.bbrs/
`
}

func confirmDestructive(stdin *os.File, stdout io.Writer, host, destination string, skip bool) (bool, error) {
	logx.Warn("bbrs mirrors your local source directory into Bitburner")
	logx.Warn(
		"remote files may be overwritten or deleted",
		"host", host,
		"destination", syncer.DisplayDestination(destination),
	)

	if skip || !isInteractive(stdin) {
		return true, nil
	}

	fmt.Fprint(stdout, "Proceed? [Y/n]: ")
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected y/yes or n/no")
	}
}

func isInteractive(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type app struct {
	ctx     context.Context
	options syncer.Options
	sync    syncFunc

	mu     sync.Mutex
	client remoteClient

	syncMu                     sync.Mutex
	syncRunning                bool
	syncPending                bool
	everConnected              bool
	waitingForConnectionLogged bool
	disconnectedPendingLogged  bool
}

type remoteClient interface {
	syncer.RemoteAPI
	Connected() bool
	Disconnected() <-chan struct{}
	Close(websocket.StatusCode, string) error
	SetDisconnectHandler(func(error))
	MarkDisconnected(error) bool
}

type syncFunc func(context.Context, syncer.RemoteAPI, syncer.Options) (syncer.Summary, error)

func newApp(ctx context.Context, options syncer.Options) *app {
	return &app{ctx: ctx, options: options, sync: syncer.Mirror}
}

func (app *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleWebSocket)
	return mux
}

func (app *app) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		// Bitburner runs in a browser origin that differs from localhost.
		InsecureSkipVerify: true,
	})
	if err != nil {
		logx.ErrorErr("websocket accept failed", err)
		return
	}

	client := bitburner.NewClient(conn)
	client.SetDisconnectHandler(func(err error) {
		app.handleClientDisconnected(client, err)
	})
	if !app.setClient(client) {
		_ = conn.Close(websocket.StatusPolicyViolation, "bbrs already has an active Bitburner connection")
		return
	}
	defer client.Close(websocket.StatusNormalClosure, "bbrs connection closed")

	app.markConnected()
	logx.Info("Bitburner connected")
	app.triggerSync("Bitburner connection")

	select {
	case <-client.Disconnected():
	case <-request.Context().Done():
		client.MarkDisconnected(request.Context().Err())
	case <-app.ctx.Done():
		client.MarkDisconnected(app.ctx.Err())
	}
}

func (app *app) setClient(client remoteClient) bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.client != nil {
		return false
	}
	app.client = client
	return true
}

func (app *app) clearClient(client remoteClient) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.client == client {
		app.client = nil
	}
}

func (app *app) activeClient() remoteClient {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.client
}

func (app *app) hasConnectedClient() bool {
	client := app.activeClient()
	return client != nil && client.Connected()
}

func (app *app) markConnected() {
	app.syncMu.Lock()
	app.everConnected = true
	app.syncMu.Unlock()
}

func (app *app) handleClientDisconnected(client remoteClient, err error) {
	app.clearClient(client)
	if err == nil {
		err = errors.New("connection closed")
	}
	logx.Warn("Bitburner connection lost", slog.Any("error", err))
}

func (app *app) triggerSync(reason string) {
	app.syncMu.Lock()
	if app.syncRunning {
		app.syncPending = true
		app.syncMu.Unlock()
		return
	}
	app.syncRunning = true
	app.syncMu.Unlock()

	currentReason := reason
	for {
		app.syncMu.Lock()
		hadPending := app.syncPending
		app.syncPending = false
		app.syncMu.Unlock()

		success := app.runOneSync(currentReason)

		if !app.finishSyncRun(success, hadPending) {
			return
		}
		currentReason = "pending sync"
	}
}

func (app *app) runOneSync(reason string) bool {
	client := app.activeClient()
	if client == nil || !client.Connected() {
		app.markSyncPendingDisconnected()
		return false
	}
	summary, err := app.sync(app.ctx, client, app.options)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		if !client.Connected() {
			app.markSyncPendingDisconnected()
			return false
		}
		logx.ErrorErr("sync failed", err, "reason", reason)
		return false
	}
	app.syncMu.Lock()
	app.disconnectedPendingLogged = false
	app.waitingForConnectionLogged = false
	app.syncMu.Unlock()
	logx.Info(
		"sync complete",
		"uploaded", summary.Uploaded,
		"skipped", summary.Skipped,
		"deleted", summary.Deleted,
		"ignored", summary.Ignored,
	)
	return true
}

func (app *app) finishSyncRun(success, hadPending bool) bool {
	connected := app.hasConnectedClient()

	app.syncMu.Lock()
	defer app.syncMu.Unlock()
	if !success && hadPending {
		app.syncPending = true
	}
	if success && app.syncPending && connected {
		return true
	}
	app.syncRunning = false
	return false
}

func (app *app) markSyncPendingDisconnected() {
	app.syncMu.Lock()
	defer app.syncMu.Unlock()
	app.syncPending = true
	if !app.everConnected {
		if app.waitingForConnectionLogged {
			return
		}
		app.waitingForConnectionLogged = true
		logx.Info("waiting for Bitburner to connect")
		return
	}
	if app.disconnectedPendingLogged {
		return
	}
	app.disconnectedPendingLogged = true
	logx.Info("Bitburner disconnected; sync pending")
}