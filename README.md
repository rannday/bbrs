# bbrs

`bbrs` mirrors a local source directory into Bitburner through the Bitburner Remote API websocket.

This tool is intentionally live and destructive. On each sync, it uploads matching local files, overwrites remote files, and deletes stale matching remote files under the configured destination.

## Build

```sh
make build
```

Or manually:

```sh
go build -ldflags "-X github.com/rannday/bbrs/internal/version.Version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bbrs ./cmd/bbrs
```

## Install Locally

```sh
make install
```

## Run

```sh
./bbrs -s ./src
./bbrs -s ./src -d scripts
./bbrs -s ./src --host home --pattern '*.txt'
./bbrs -s ./src -l 127.0.0.1 -p 12525
./bbrs -s ./src --dry-run
./bbrs -s ./src --once
./bbrs -s ./src --verbose
```

`bbrs` starts a websocket server. In Bitburner, open `Options -> Remote API`, set host `127.0.0.1` and port `12525`, then connect.

On first connection, `bbrs` performs a full sync. It keeps running and watches the source directory for changes. Saves, creates, and deletes trigger incremental syncs when possible, otherwise a full mirror.

## Options

On sync, `bbrs` logs `uploaded`, `skipped`, `deleted`, `ignored`, and `failed` counts. Unchanged local files are skipped using a persistent upload cache in `<source>/.bbrs/cache.json` when the remote metadata still contains the file, so a remotely deleted file is uploaded again on the next sync.

`--dry-run` still requires a Bitburner Remote API connection because it reads remote metadata, but it does not call `pushFile`, does not call `deleteFile`, and does not update the local upload cache. Counts mean would-upload, would-skip, would-delete, and ignored.

`--once` waits for one successful sync after Bitburner connects, then exits.

`--verbose` enables debug logging.

By default, `--listen` accepts only loopback addresses such as `127.0.0.1`, `::1`, and `localhost`. Use `--allow-remote-listen` to bind a non-loopback address such as `0.0.0.0`. This can let remote browser origins connect and trigger destructive sync operations.

## Config File

Optional settings can live in `<source>/.bbrs/config.toml` or `<source>/.bbrs/config.json`. CLI flags override file values.

Example `config.json`:

```json
{
  "listen": "127.0.0.1",
  "port": 12525,
  "destination": "scripts",
  "host": "home",
  "patterns": ["*.txt", "*.ns"],
  "ignored_dirs": ["vendor"],
  "verbose": false,
  "once": false
}
```

Example `config.toml`:

```toml
listen = "127.0.0.1"
port = 12525
destination = "scripts"
host = "home"
patterns = ["*.txt", "*.ns"]
ignored_dirs = ["vendor"]
```

## Persistent Cache

Upload cache is stored in `<source>/.bbrs/cache.json` and survives restarts. The cache is updated after successful uploads and deletes.

## Logging

`bbrs` uses [go-log](https://github.com/rannday/go-log) for structured console and file logging.

Default log location:

- Unix-like systems: `/var/log/bbrs/` when that directory already exists.
- Otherwise: `<source>/.bbrs/`
- Windows: `<source>/.bbrs/`

Each run writes to `bbrs_log_<timestamp>.log`. Logs rotate at 10 MiB with up to five backups.

Override with `--log-dir`:

```sh
./bbrs -s ./src --log-dir /tmp/bbrs-logs
```

`bbrs` always writes `bbrs_log_<timestamp>.log` inside the chosen directory and creates the directory when needed. The `.bbrs` directory is ignored during sync.

## Patterns

Default included files:

```text
*.js
*.ts
```

`*.d.ts` is always excluded.

`--pattern` expands the default include set. It does not replace defaults. Repeated and comma-separated patterns both work.

```sh
./bbrs -s ./src --pattern '*.txt'
./bbrs -s ./src --pattern '*.js,*.ts,*.ns'
./bbrs -s ./src --pattern '*.script' --pattern '*.txt'
```

Patterns use Go `path.Match` shell-style glob rules and match slash-normalized relative paths and base filenames.

## Mirror Rules

Each sync:

1. Walks `--source`.
2. Skips ignored directories: `.bbrs`, `.git`, `target`, `node_modules`, `dist`, `build`, `.zed`, `.vscode`, `.idea`, `coverage`, `tmp`, `temp`, plus any `--ignore-dir` or `ignored_dirs` config values.
3. Uploads every desired local file to `--host` under `--destination`.
4. Fetches remote metadata with `getAllFileMetadata`.
5. Deletes stale remote files only when they are under `--destination` and match active patterns.

Individual upload and delete failures are logged and counted in `failed`; other files still sync.

Remote paths are rejected when absolute, empty, using Windows drive prefixes, or containing `..`.

## Development

```sh
make test
make fmt
make vet
make tidy
```