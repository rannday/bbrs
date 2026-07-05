package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	logx "github.com/rannday/go-log"
)

const (
	defaultMaxSizeBytes = 10 * 1024 * 1024
	defaultMaxBackups   = 5
)

var unixSystemLogDir = "/var/log/bbrs"

// ResolveLogPath picks the log file path from an explicit --log-dir value and defaults.
func ResolveLogPath(logdir, source string) (string, error) {
	dir, err := resolveLogDir(logdir, source)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, logFileName()), nil
}

func resolveLogDir(logdir, source string) (string, error) {
	if logdir != "" {
		info, err := os.Stat(logdir)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("log directory %q is not a directory", logdir)
			}
			return logdir, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat log directory %q: %w", logdir, err)
		}
		if err := os.MkdirAll(logdir, 0o755); err != nil {
			return "", fmt.Errorf("create log directory %q: %w", logdir, err)
		}
		return logdir, nil
	}

	return defaultLogDir(source)
}

func defaultLogDir(source string) (string, error) {
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(unixSystemLogDir); err == nil && info.IsDir() {
			return unixSystemLogDir, nil
		}
	}

	dir := filepath.Join(source, ".bbrs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create log directory %q: %w", dir, err)
	}
	return dir, nil
}

func logFileName() string {
	return fmt.Sprintf("bbrs_log_%s.log", time.Now().UTC().Format("20060102T150405"))
}

// Configure installs the global logger with console and rotating file output.
func Configure(logPath string, level slog.Level) error {
	return logx.Configure(logx.Config{
		Level:            level,
		Console:          true,
		FilePath:         logPath,
		FileMaxSizeBytes: defaultMaxSizeBytes,
		FileMaxBackups:   defaultMaxBackups,
		StacktraceLevel:  slog.LevelError,
	})
}
