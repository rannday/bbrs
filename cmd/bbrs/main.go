package main

import (
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
	"github.com/rannday/bbrs/internal/bitburner"
	"github.com/rannday/bbrs/internal/config"
	"github.com/rannday/bbrs/internal/logging"
	"github.com/rannday/bbrs/internal/syncer"
	"github.com/rannday/bbrs/internal/version"
	"github.com/rannday/bbrs/internal/watch"
	logx "github.com/rannday/go-log"
)

type cliConfig struct {
	Source      string
	Listen      string
	Port        int
	Destination string
	Target      string
	Include     []string
	Ignore      []string
	Verbose     bool
	Version     bool
	LogDir      string
}

type listFlags []string

func (flags *listFlags) String() string {
	return strings.Join(*flags, ",")
}

func (flags *listFlags) Set(value string) error {
	*flags = append(*flags, value)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		logx.ErrorErr("bbrs failed", err)
		os.Exit(1)
	}
}

func run(args []string, _ *os.File, stdout, stderr io.Writer) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if cfg.Version {
		fmt.Fprintln(stdout, version.Version)
		return nil
	}

	logPath, err := logging.ResolveLogPath(cfg.LogDir, cfg.Source)
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	if err := logging.Configure(logPath, level); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}
	logx.Info("logging enabled", "path", logPath, "verbose", cfg.Verbose)

	patterns, err := syncer.NewPatterns(cfg.Include)
	if err != nil {
		return err
	}
	ignored, err := syncer.NewIgnoredPatterns(cfg.Ignore)
	if err != nil {
		return err
	}

	cachePath := syncer.CachePath(cfg.Source)
	state, err := syncer.LoadState(cachePath)
	if err != nil {
		return err
	}
	logx.Info("loaded upload cache", "path", cachePath, "entries", len(state.UploadCache))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	options := syncer.Options{
		Source:      cfg.Source,
		Destination: cfg.Destination,
		Host:        cfg.Target,
		Patterns:    patterns,
		Ignored:     ignored,
		State:       state,
		CachePath:   cachePath,
	}

	app := newApp(ctx, options)

	go func() {
		if err := watch.Poll(ctx, cfg.Source, patterns, ignored, 750*time.Millisecond, 200*time.Millisecond, func(changes syncer.ChangeSet) {
			app.queueSync("local file change", changes)
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

func parseConfig(args []string, output io.Writer) (cliConfig, error) {
	var cfg cliConfig
	cfg.Listen = "127.0.0.1"
	cfg.Port = 12525
	cfg.Destination = "bbrs"
	cfg.Target = "home"

	fs := flag.NewFlagSet("bbrs", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprint(output, helpText())
	}

	var help bool
	var include listFlags
	var ignored listFlags
	fs.BoolVar(&help, "h", false, "show help")
	fs.BoolVar(&help, "help", false, "show help")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "enable debug logging")
	fs.BoolVar(&cfg.Version, "v", false, "print version and exit")
	fs.BoolVar(&cfg.Version, "version", false, "print version and exit")
	fs.StringVar(&cfg.Source, "s", "", "local source directory to sync")
	fs.StringVar(&cfg.Source, "source", "", "local source directory to sync")
	fs.StringVar(&cfg.Listen, "l", cfg.Listen, "listen address")
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "listen address")
	fs.IntVar(&cfg.Port, "p", cfg.Port, "listen port")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "listen port")
	fs.StringVar(&cfg.Destination, "d", cfg.Destination, "destination directory inside Bitburner")
	fs.StringVar(&cfg.Destination, "destination", cfg.Destination, "destination directory inside Bitburner")
	fs.StringVar(&cfg.Target, "t", cfg.Target, "target Bitburner host")
	fs.StringVar(&cfg.Target, "target", cfg.Target, "target Bitburner host")
	fs.Var(&include, "include", "additional filename pattern to include")
	fs.Var(&ignored, "ignore", "additional filename or directory pattern to ignore during sync")
	fs.StringVar(&cfg.LogDir, "log-dir", "", "directory for log files")

	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	if help {
		fs.Usage()
		return cliConfig{}, flag.ErrHelp
	}
	if cfg.Version {
		return cfg, nil
	}
	cfg.Include = append([]string{}, include...)
	cfg.Ignore = append([]string{}, ignored...)
	if cfg.Source == "" {
		return cliConfig{}, fmt.Errorf("--source is required")
	}
	info, err := os.Stat(cfg.Source)
	if err != nil {
		return cliConfig{}, fmt.Errorf("source %q: %w", cfg.Source, err)
	}
	if !info.IsDir() {
		return cliConfig{}, fmt.Errorf("source %q is not a directory", cfg.Source)
	}
	source, err := filepath.Abs(cfg.Source)
	if err != nil {
		return cliConfig{}, fmt.Errorf("resolve source %q: %w", cfg.Source, err)
	}
	cfg.Source = filepath.Clean(source)

	fileCfg, err := config.Load(cfg.Source)
	if err != nil {
		return cliConfig{}, fmt.Errorf("load config: %w", err)
	}
	applyFileConfig(&cfg, fileCfg)

	normalized, err := syncer.NormalizeRemotePath(cfg.Destination)
	if err != nil {
		return cliConfig{}, fmt.Errorf("invalid destination %q: %w", cfg.Destination, err)
	}
	cfg.Destination = normalized
	return cfg, nil
}

func applyFileConfig(cfg *cliConfig, file config.File) {
	if file.Listen != "" {
		cfg.Listen = file.Listen
	}
	if file.Port != nil {
		cfg.Port = *file.Port
	}
	if file.Destination != "" {
		cfg.Destination = file.Destination
	}
	if file.Target != "" {
		cfg.Target = file.Target
	}
	if len(file.Include) > 0 && len(cfg.Include) == 0 {
		cfg.Include = append([]string{}, file.Include...)
	}
	if file.LogDir != "" && cfg.LogDir == "" {
		cfg.LogDir = file.LogDir
	}
	if file.Verbose != nil && !cfg.Verbose {
		cfg.Verbose = *file.Verbose
	}
	if len(file.Ignore) > 0 && len(cfg.Ignore) == 0 {
		cfg.Ignore = append([]string{}, file.Ignore...)
	}
}

func helpText() string {
	return `Usage:
  bbrs -s ./source-dir [options]

Options:
  -s, --source               Local source directory to sync. Required
  -d, --destination          Destination directory inside Bitburner. Default: /bbrs/
  -l, --listen               Listen address. Default: 127.0.0.1
  -p, --port                 Listen port. Default: 12525
  -t, --target               Target Bitburner host. Default: home
      --include              Additional filename patterns to include.
      --ignore               Additional filename or directory patterns to ignore.
      --log-dir              Directory for log files.
      --verbose              Enable debug logging.
  -v, --version              Print version and exit.
  -h, --help                 Show help.

Config file:
  Optional settings in <source>/.bbrs/config.toml.
  CLI flags override config file values.

Persistent cache:
  Upload cache stored in <source>/.bbrs/cache.json across restarts.

Include examples:
  --include '*.txt'
  --include '*.js,*.ts,*.ns'
  --include '*.script' --include '*.txt'

Ignore examples:
  --ignore dist
  --ignore dist,tmp,*.map
  --ignore vendor --ignore '*.map'

Logging:
  Default: /var/log/bbrs/ on *nix when present, otherwise <source>/.bbrs/
`
}

type syncJob struct {
	reason  string
	changes syncer.ChangeSet
}

type app struct {
	ctx     context.Context
	options syncer.Options
	syncFn  syncFunc

	mu     sync.Mutex
	client remoteClient

	workerMu sync.Mutex
	running  bool
	pending  *syncJob

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

type syncFunc func(context.Context, syncer.RemoteAPI, syncer.Options, syncer.ChangeSet) (syncer.Result, error)

func newApp(ctx context.Context, options syncer.Options) *app {
	return &app{
		ctx:     ctx,
		options: options,
		syncFn:  defaultSyncFn,
	}
}

func defaultSyncFn(ctx context.Context, api syncer.RemoteAPI, options syncer.Options, changes syncer.ChangeSet) (syncer.Result, error) {
	if len(changes.Modified) == 0 && len(changes.Deleted) == 0 {
		return syncer.Mirror(ctx, api, options)
	}
	return syncer.SyncChanges(ctx, api, options, changes)
}

func (app *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleWebSocket)
	return mux
}

func (app *app) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
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
	app.queueSync("Bitburner connection", syncer.ChangeSet{})

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
	app.workerMu.Lock()
	app.everConnected = true
	app.workerMu.Unlock()
}

func (app *app) handleClientDisconnected(client remoteClient, err error) {
	app.clearClient(client)
	if err == nil {
		err = errors.New("connection closed")
	}
	logx.Warn("Bitburner connection lost", slog.Any("error", err))
}

func mergeJobs(current, incoming *syncJob) *syncJob {
	if current == nil {
		return incoming
	}
	if incoming == nil {
		return current
	}
	return &syncJob{
		reason: incoming.reason,
		changes: syncer.ChangeSet{
			Modified: append(append([]string{}, current.changes.Modified...), incoming.changes.Modified...),
			Deleted:  append(append([]string{}, current.changes.Deleted...), incoming.changes.Deleted...),
		},
	}
}

func (app *app) queueSync(reason string, changes syncer.ChangeSet) {
	job := &syncJob{reason: reason, changes: changes}

	app.workerMu.Lock()
	app.pending = mergeJobs(app.pending, job)
	if app.running {
		app.workerMu.Unlock()
		return
	}
	if !app.hasConnectedClient() {
		app.workerMu.Unlock()
		app.markSyncPendingDisconnected()
		return
	}
	app.running = true
	job = app.pending
	app.pending = nil
	app.workerMu.Unlock()

	go app.syncLoop(job)
}

func (app *app) syncLoop(job *syncJob) {
	for {
		if !app.hasConnectedClient() {
			app.workerMu.Lock()
			app.running = false
			app.workerMu.Unlock()
			app.markSyncPendingDisconnected()
			return
		}

		app.runOneSync(job.reason, job.changes)

		app.workerMu.Lock()
		if !app.hasConnectedClient() {
			app.running = false
			app.workerMu.Unlock()
			app.markSyncPendingDisconnected()
			return
		}
		job = app.pending
		app.pending = nil
		if job == nil {
			app.running = false
			app.workerMu.Unlock()
			return
		}
		app.workerMu.Unlock()
	}
}

func (app *app) runOneSync(reason string, changes syncer.ChangeSet) bool {
	client := app.activeClient()
	if client == nil || !client.Connected() {
		app.markSyncPendingDisconnected()
		return false
	}

	result, err := app.syncFn(app.ctx, client, app.options, changes)
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

	for _, syncErr := range result.Errors {
		logx.Warn("sync issue", slog.Any("error", syncErr), "reason", reason)
	}

	app.workerMu.Lock()
	app.disconnectedPendingLogged = false
	app.waitingForConnectionLogged = false
	app.workerMu.Unlock()

	logx.Info(
		"sync complete",
		"reason", reason,
		"uploaded", result.Summary.Uploaded,
		"skipped", result.Summary.Skipped,
		"deleted", result.Summary.Deleted,
		"ignored", result.Summary.Ignored,
		"failed", result.Summary.Failed,
	)
	return result.Summary.Failed == 0
}

func (app *app) markSyncPendingDisconnected() {
	app.workerMu.Lock()
	defer app.workerMu.Unlock()

	if !app.running {
		// queueSync owns pending state when idle; reconnect will re-trigger.
	}
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

// triggerSync queues a full mirror sync. Used by tests.
func (app *app) triggerSync(reason string) {
	app.queueSync(reason, syncer.ChangeSet{})
}
