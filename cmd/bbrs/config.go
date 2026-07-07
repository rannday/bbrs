package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rannday/bbrs/internal/config"
	"github.com/rannday/bbrs/internal/syncer"
	envx "github.com/rannday/go-env"
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

type partialConfig struct {
	Source      *string  `env:"BBRS_SOURCE"`
	Listen      *string  `env:"BBRS_LISTEN"`
	Port        *int     `env:"BBRS_PORT"`
	Destination *string  `env:"BBRS_DESTINATION"`
	Target      *string  `env:"BBRS_TARGET"`
	Include     []string `env:"BBRS_INCLUDE"`
	Ignore      []string `env:"BBRS_IGNORE"`
	LogDir      *string  `env:"BBRS_LOG_DIR"`
	Verbose     *bool    `env:"BBRS_VERBOSE"`
}

type configPaths struct {
	SystemEnvPath string
	UserEnvPath   string
}

var defaultConfigPaths = configPaths{
	SystemEnvPath: "/etc/bbrs/env",
	UserEnvPath:   "~/conf/bbrs/env",
}

var bbrsEnvKeys = []string{
	"BBRS_SOURCE",
	"BBRS_LISTEN",
	"BBRS_PORT",
	"BBRS_DESTINATION",
	"BBRS_TARGET",
	"BBRS_INCLUDE",
	"BBRS_IGNORE",
	"BBRS_LOG_DIR",
	"BBRS_VERBOSE",
}

type listFlags []string

func (flags *listFlags) String() string {
	return strings.Join(*flags, ",")
}

func (flags *listFlags) Set(value string) error {
	*flags = append(*flags, value)
	return nil
}

func parseConfig(args []string, output io.Writer) (cliConfig, error) {
	return parseConfigWithPaths(args, output, defaultConfigPaths)
}

func parseConfigWithPaths(args []string, output io.Writer, paths configPaths) (cliConfig, error) {
	cli, explicit, err := parseCLI(args, output)
	if err != nil {
		return cliConfig{}, err
	}
	if cli.Version {
		return cli, nil
	}

	cfg := defaultCLIConfig()
	if err := applyEnvFile(&cfg, paths.SystemEnvPath); err != nil {
		return cliConfig{}, err
	}
	if err := applyEnvFile(&cfg, paths.UserEnvPath); err != nil {
		return cliConfig{}, err
	}

	processEnv, err := loadProcessEnv()
	if err != nil {
		return cliConfig{}, err
	}

	sourceCfg := cfg
	applyPartialSource(&sourceCfg, processEnv)
	applyCLISource(&sourceCfg, cli, explicit)
	projectSource, err := normalizeSource(sourceCfg.Source)
	if err != nil {
		return cliConfig{}, err
	}

	fileCfg, err := config.Load(projectSource)
	if err != nil {
		return cliConfig{}, fmt.Errorf("load config: %w", err)
	}
	applyFileConfig(&cfg, fileCfg)
	applyPartialConfig(&cfg, processEnv)
	applyExplicitCLIConfig(&cfg, cli, explicit)

	source, err := normalizeSource(cfg.Source)
	if err != nil {
		return cliConfig{}, err
	}
	cfg.Source = source

	normalized, err := syncer.NormalizeRemotePath(cfg.Destination)
	if err != nil {
		return cliConfig{}, fmt.Errorf("invalid destination %q: %w", cfg.Destination, err)
	}
	cfg.Destination = normalized
	return cfg, nil
}

func parseCLI(args []string, output io.Writer) (cliConfig, map[string]bool, error) {
	cli := defaultCLIConfig()

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
	fs.BoolVar(&cli.Verbose, "verbose", false, "enable debug logging")
	fs.BoolVar(&cli.Version, "v", false, "print version and exit")
	fs.BoolVar(&cli.Version, "version", false, "print version and exit")
	fs.StringVar(&cli.Source, "s", "", "local source directory to sync")
	fs.StringVar(&cli.Source, "source", "", "local source directory to sync")
	fs.StringVar(&cli.Listen, "l", cli.Listen, "listen address")
	fs.StringVar(&cli.Listen, "listen", cli.Listen, "listen address")
	fs.IntVar(&cli.Port, "p", cli.Port, "listen port")
	fs.IntVar(&cli.Port, "port", cli.Port, "listen port")
	fs.StringVar(&cli.Destination, "d", cli.Destination, "destination directory inside Bitburner")
	fs.StringVar(&cli.Destination, "destination", cli.Destination, "destination directory inside Bitburner")
	fs.StringVar(&cli.Target, "t", cli.Target, "target Bitburner host")
	fs.StringVar(&cli.Target, "target", cli.Target, "target Bitburner host")
	fs.Var(&include, "include", "additional filename pattern to include")
	fs.Var(&ignored, "ignore", "additional filename or directory pattern to ignore during sync")
	fs.StringVar(&cli.LogDir, "log-dir", "", "directory for log files")

	if err := fs.Parse(args); err != nil {
		return cliConfig{}, nil, err
	}
	explicit := explicitFlags(fs)
	if help {
		fs.Usage()
		return cliConfig{}, nil, flag.ErrHelp
	}
	cli.Include = append([]string{}, include...)
	cli.Ignore = append([]string{}, ignored...)
	return cli, explicit, nil
}

func defaultCLIConfig() cliConfig {
	return cliConfig{
		Listen:      "127.0.0.1",
		Port:        12525,
		Destination: "bbrs",
		Target:      "home",
	}
}

func explicitFlags(fs *flag.FlagSet) map[string]bool {
	explicit := make(map[string]bool)
	fs.Visit(func(flag *flag.Flag) {
		explicit[canonicalFlagName(flag.Name)] = true
	})
	return explicit
}

func canonicalFlagName(name string) string {
	switch name {
	case "s":
		return "source"
	case "l":
		return "listen"
	case "p":
		return "port"
	case "d":
		return "destination"
	case "t":
		return "target"
	case "v":
		return "version"
	case "h":
		return "help"
	default:
		return name
	}
}

func normalizeSource(source string) (string, error) {
	if source == "" {
		return "", fmt.Errorf("--source is required")
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("source %q: %w", source, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source %q is not a directory", source)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve source %q: %w", source, err)
	}
	return filepath.Clean(abs), nil
}

func applyEnvFile(cfg *cliConfig, path string) error {
	expanded, err := expandHome(path)
	if err != nil {
		return err
	}
	part, err := loadEnvFile(expanded)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load env file %q: %w", expanded, err)
	}
	applyPartialConfig(cfg, part)
	return nil
}

func expandHome(path string) (string, error) {
	if path == "" || path == "~" {
		if path == "" {
			return "", nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", path, err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", path, err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func loadProcessEnv() (partialConfig, error) {
	var part partialConfig
	if err := envx.LoadInto(&part); err != nil {
		return partialConfig{}, fmt.Errorf("load process env: %w", err)
	}
	return part, nil
}

func loadEnvFile(path string) (partialConfig, error) {
	values, err := parseDotEnv(path)
	if err != nil {
		return partialConfig{}, err
	}
	return partialConfigFromMap(values, path)
}

func partialConfigFromMap(values map[string]string, path string) (partialConfig, error) {
	var part partialConfig
	for key, value := range values {
		switch key {
		case "BBRS_SOURCE":
			part.Source = stringPtr(value)
		case "BBRS_LISTEN":
			part.Listen = stringPtr(value)
		case "BBRS_PORT":
			port, err := strconv.Atoi(value)
			if err != nil {
				return partialConfig{}, fmt.Errorf("%s: invalid value for %s: %w", path, key, err)
			}
			part.Port = intPtr(port)
		case "BBRS_DESTINATION":
			part.Destination = stringPtr(value)
		case "BBRS_TARGET":
			part.Target = stringPtr(value)
		case "BBRS_INCLUDE":
			part.Include = splitEnvList(value)
		case "BBRS_IGNORE":
			part.Ignore = splitEnvList(value)
		case "BBRS_LOG_DIR":
			part.LogDir = stringPtr(value)
		case "BBRS_VERBOSE":
			verbose, err := strconv.ParseBool(value)
			if err != nil {
				return partialConfig{}, fmt.Errorf("%s: invalid value for %s: %w", path, key, err)
			}
			part.Verbose = boolPtr(verbose)
		}
	}
	return part, nil
}

func splitEnvList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parseDotEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if lineNum == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("dotenv %s:%d: invalid line: %q", path, lineNum, line)
		}
		key := strings.TrimSpace(parts[0])
		key = strings.TrimPrefix(key, "export ")
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("dotenv %s:%d: invalid line: %q", path, lineNum, line)
		}

		value := strings.TrimSpace(parts[1])
		value = stripInlineComment(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dotenv %q: %w", path, err)
	}
	return values, nil
}

func stripInlineComment(value string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t' || onlyWhitespace(value[:i])) {
				return strings.TrimSpace(value[:i])
			}
		}
	}
	return strings.TrimSpace(value)
}

func onlyWhitespace(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] != ' ' && value[i] != '\t' {
			return false
		}
	}
	return true
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
	if len(file.Include) > 0 {
		cfg.Include = append([]string{}, file.Include...)
	}
	if file.LogDir != "" {
		cfg.LogDir = file.LogDir
	}
	if file.Verbose != nil {
		cfg.Verbose = *file.Verbose
	}
	if len(file.Ignore) > 0 {
		cfg.Ignore = append([]string{}, file.Ignore...)
	}
}

func applyPartialConfig(cfg *cliConfig, part partialConfig) {
	applyPartialSource(cfg, part)
	if part.Listen != nil {
		cfg.Listen = *part.Listen
	}
	if part.Port != nil {
		cfg.Port = *part.Port
	}
	if part.Destination != nil {
		cfg.Destination = *part.Destination
	}
	if part.Target != nil {
		cfg.Target = *part.Target
	}
	if part.Include != nil {
		cfg.Include = append([]string{}, part.Include...)
	}
	if part.Ignore != nil {
		cfg.Ignore = append([]string{}, part.Ignore...)
	}
	if part.LogDir != nil {
		cfg.LogDir = *part.LogDir
	}
	if part.Verbose != nil {
		cfg.Verbose = *part.Verbose
	}
}

func applyPartialSource(cfg *cliConfig, part partialConfig) {
	if part.Source != nil {
		cfg.Source = *part.Source
	}
}

func applyCLISource(cfg *cliConfig, cli cliConfig, explicit map[string]bool) {
	if explicit["source"] {
		cfg.Source = cli.Source
	}
}

func applyExplicitCLIConfig(cfg *cliConfig, cli cliConfig, explicit map[string]bool) {
	applyCLISource(cfg, cli, explicit)
	if explicit["listen"] {
		cfg.Listen = cli.Listen
	}
	if explicit["port"] {
		cfg.Port = cli.Port
	}
	if explicit["destination"] {
		cfg.Destination = cli.Destination
	}
	if explicit["target"] {
		cfg.Target = cli.Target
	}
	if explicit["include"] {
		cfg.Include = append([]string{}, cli.Include...)
	}
	if explicit["ignore"] {
		cfg.Ignore = append([]string{}, cli.Ignore...)
	}
	if explicit["log-dir"] {
		cfg.LogDir = cli.LogDir
	}
	if explicit["verbose"] {
		cfg.Verbose = cli.Verbose
	}
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func helpText() string {
	return `Usage:
  bbrs -s ./source-dir [options]

Options:
  -s, --source               Local source directory to sync. Required unless set by BBRS_SOURCE
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

Configuration precedence:
  coded defaults < /etc/bbrs/env < ~/conf/bbrs/env < <source>/.bbrs/config.toml < process env vars < CLI args

Environment:
  Supported variables: BBRS_SOURCE, BBRS_LISTEN, BBRS_PORT, BBRS_DESTINATION, BBRS_TARGET,
  BBRS_INCLUDE, BBRS_IGNORE, BBRS_LOG_DIR, BBRS_VERBOSE.
  BBRS_INCLUDE and BBRS_IGNORE are comma-separated lists.

Config file:
  Optional settings in <source>/.bbrs/config.toml.

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
