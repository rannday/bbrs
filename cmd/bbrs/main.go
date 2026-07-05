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
	"github.com/rannday/bbrs/internal/bitburner"
	"github.com/rannday/bbrs/internal/config"
	"github.com/rannday/bbrs/internal/logging"
	"github.com/rannday/bbrs/internal/syncer"
	"github.com/rannday/bbrs/internal/version"
	"github.com/rannday/bbrs/internal/watch"
	logx "github.com/rannday/go-log"
)

type cliConfig struct {
	Source            string
	Listen            string
	Port              int
	Destination       string
	Host              string
	Patterns          []string
	IgnoredDirs       []string
	Yes               bool
	DryRun            bool
	Once              bool
	Verbose           bool
	AllowRemoteListen bool
	Version           bool
	LogDir            string
}

type patternFlags []string

func (flags *patternFlags) String() string {
	return strings.Join(*flags, ",")
}

func (flags *patternFlags) Set(value string) error {
	*flags = append(*flags, value)
	return nil
}

type ignoredDirFlags []string

func (flags *ignoredDirFlags) String() string {
	return strings.Join(*flags, ",")
}

func (flags *ignoredDirFlags) Set(value string) error {
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
	if cfg.AllowRemoteListen && !isLoopbackListenAddress(cfg.Listen) {
		logx.Warn(
			"non-loopback listen address enabled; remote browser origins may connect and trigger destructive sync operations",
			"listen", cfg.Listen,
		)
	}

	patterns, err := syncer.NewPatterns(cfg.Patterns)
	if err != nil {
		return err
	}
	ignored := syncer.NewIgnoredDirs(cfg.IgnoredDirs)

	cachePath := syncer.CachePath(cfg.Source)
	state, err := syncer.LoadState(cachePath)
	if err != nil {
		return err
	}
	logx.Info("loaded upload cache", "path", cachePath, "entries", len(state.UploadCache))

	if !cfg.DryRun {
		proceed, err := confirmDestructive(stdin, stdout, cfg.Host, cfg.Destination, cfg.Yes)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	options := syncer.Options{
		Source:      cfg.Source,
		Destination: cfg.Destination,
		Host:        cfg.Host,
		Patterns:    patterns,
		Ignored:     ignored,
		State:       state,
		CachePath:   cachePath,
		DryRun:      cfg.DryRun,
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

	if cfg.Once {
		if err := app.waitForSuccessfulSync(ctx, 2*time.Minute); err != nil {
			return err
		}
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	}

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
	cfg.Host = "home"

	fs := flag.NewFlagSet("bbrs", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		fmt.Fprint(output, helpText())
	}

	var help bool
	var patterns patternFlags
	var ignoredDirs ignoredDirFlags
	fs.BoolVar(&help, "h", false, "show help")
	fs.BoolVar(&help, "help", false, "show help")
	fs.BoolVar(&cfg.Yes, "y", false, "skip destructive-operation confirmation")
	fs.BoolVar(&cfg.Yes, "yes", false, "skip destructive-operation confirmation")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "build sync plan without uploading, deleting, or updating the cache")
	fs.BoolVar(&cfg.Once, "once", false, "sync once after Bitburner connects, then exit")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "enable debug logging")
	fs.BoolVar(&cfg.AllowRemoteListen, "allow-remote-listen", false, "allow listening on non-loopback addresses")
	fs.BoolVar(&cfg.Version, "version", false, "print version and exit")
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
	fs.Var(&ignoredDirs, "ignore-dir", "additional directory name to ignore during sync")
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

	if !isLoopbackListenAddress(cfg.Listen) && !cfg.AllowRemoteListen {
		return cliConfig{}, fmt.Errorf("listen address %q is not loopback; use --allow-remote-listen to allow remote browser origins", cfg.Listen)
	}
	if cfg.Destination != "" {
		normalized, err := syncer.NormalizeRemotePath(cfg.Destination)
		if err != nil {
			return cliConfig{}, fmt.Errorf("invalid destination %q: %w", cfg.Destination, err)
		}
		cfg.Destination = normalized
	}
	cfg.Patterns = append([]string{}, patterns...)
	cfg.IgnoredDirs = append([]string{}, ignoredDirs...)
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
	if file.Host != "" {
		cfg.Host = file.Host
	}
	if len(file.Patterns) > 0 && len(cfg.Patterns) == 0 {
		cfg.Patterns = append([]string{}, file.Patterns...)
	}
	if file.LogDir != "" && cfg.LogDir == "" {
		cfg.LogDir = file.LogDir
	}
	if file.AllowRemoteListen != nil && !cfg.AllowRemoteListen {
		cfg.AllowRemoteListen = *file.AllowRemoteListen
	}
	if file.DryRun != nil && !cfg.DryRun {
		cfg.DryRun = *file.DryRun
	}
	if file.Verbose != nil && !cfg.Verbose {
		cfg.Verbose = *file.Verbose
	}
	if file.Once != nil && !cfg.Once {
		cfg.Once = *file.Once
	}
	if file.Yes != nil && !cfg.Yes {
		cfg.Yes = *file.Yes
	}
	if len(file.IgnoredDirs) > 0 && len(cfg.IgnoredDirs) == 0 {
		cfg.IgnoredDirs = append([]string{}, file.IgnoredDirs...)
	}
}

func helpText() string {
	return `Usage:
  bbrs -s ./source-dir [options]

Options:
  -h, --help                 Show help.
      --version              Print version and exit.
  -s, --source               Local source directory to sync. Required.
  -l, --listen               Listen address. Default: 127.0.0.1.
  -p, --port                 Listen port. Default: 12525.
  -d, --destination          Destination directory inside Bitburner. Default: empty/root.
      --host                 Destination Bitburner host. Default: home.
      --pattern              Additional filename patterns to include.
      --ignore-dir           Additional directory name to ignore during sync.
      --log-dir              Directory for log files.
      --dry-run              Build the sync plan without uploading, deleting, or updating cache.
      --once                 Sync once after Bitburner connects, then exit.
      --verbose              Enable debug logging.
      --allow-remote-listen  Allow listening on non-loopback addresses.
  -y, --yes                  Skip destructive-operation confirmation.

Config file:
  Optional settings in <source>/.bbrs/config.toml or <source>/.bbrs/config.json.
  CLI flags override config file values.

Persistent cache:
  Upload cache stored in <source>/.bbrs/cache.json across restarts.

Pattern examples:
  --pattern '*.txt'
  --pattern '*.js,*.ts,*.ns'
  --pattern '*.script' --pattern '*.txt'

Logging:
  Default: /var/log/bbrs/ on Unix when present, otherwise <source>/.bbrs/
`
}

var stdinIsInteractive = isInteractive

func confirmDestructive(stdin *os.File, stdout io.Writer, host, destination string, skip bool) (bool, error) {
	logx.Warn("bbrs mirrors your local source directory into Bitburner")
	logx.Warn(
		"remote files may be overwritten or deleted; stale matching files under the destination are removed",
		"host", host,
		"destination", syncer.DisplayDestination(destination),
	)

	if skip {
		return true, nil
	}
	if !stdinIsInteractive(stdin) {
		return false, fmt.Errorf("refusing destructive sync in non-interactive mode without --yes")
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

func isLoopbackListenAddress(address string) bool {
	if strings.EqualFold(address, "localhost") {
		return true
	}
	ip := net.ParseIP(address)
	return ip != nil && ip.IsLoopback()
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

	statusMu   sync.Mutex
	everSyncOK bool
	syncOKCh   chan struct{}

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
		ctx:      ctx,
		options:  options,
		syncFn:   defaultSyncFn,
		syncOKCh: make(chan struct{}, 1),
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

	message := "sync complete"
	if app.options.DryRun {
		message = "dry run complete"
	}
	logx.Info(
		message,
		"reason", reason,
		"dry_run", app.options.DryRun,
		"uploaded", result.Summary.Uploaded,
		"skipped", result.Summary.Skipped,
		"deleted", result.Summary.Deleted,
		"ignored", result.Summary.Ignored,
		"failed", result.Summary.Failed,
	)

	if result.Summary.Failed == 0 {
		app.statusMu.Lock()
		if !app.everSyncOK {
			app.everSyncOK = true
			select {
			case app.syncOKCh <- struct{}{}:
			default:
			}
		}
		app.statusMu.Unlock()
	}
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

func (app *app) waitForSuccessfulSync(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-app.syncOKCh:
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out after %s waiting for successful sync", timeout)
	}
}

// triggerSync queues a full mirror sync. Used by tests.
func (app *app) triggerSync(reason string) {
	app.queueSync(reason, syncer.ChangeSet{})
}
