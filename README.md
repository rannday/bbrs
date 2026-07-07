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
./bbrs -s ./src --target home --include '*.txt'
./bbrs -s ./src --ignore dist --ignore tmp
./bbrs -s ./src -l 127.0.0.1 -p 12525
./bbrs -s ./src --verbose
./bbrs -v
```

`bbrs` starts a websocket server. In Bitburner, open `Options -> Remote API`, set host `127.0.0.1` and port `12525`, then connect.

On first connection, `bbrs` performs a full sync. It keeps running and watches the source directory for changes. Saves, creates, and deletes trigger incremental syncs when possible, otherwise a full mirror.

## Options

On sync, `bbrs` logs `uploaded`, `skipped`, `deleted`, `ignored`, and `failed` counts. Unchanged local files are skipped using a persistent upload cache in `<source>/.bbrs/cache.json` when the remote metadata still contains the file, so a remotely deleted file is uploaded again on the next sync.

`--verbose` enables debug logging.

`-v, --version` prints the version and exits.

`--listen` defaults to `127.0.0.1`. If you set another listen address, `bbrs` uses it.

## Configuration

Configuration loads in this order, lowest to highest precedence:

```text
coded defaults < /etc/bbrs/env < ~/conf/bbrs/env < <source>/.bbrs/config.toml < process env vars < CLI args
```

Missing env files are ignored. Malformed env files fail with the file path and line number. `~` in `~/conf/bbrs/env` is expanded with the current user's home directory.

Supported environment variables:

```text
BBRS_SOURCE
BBRS_LISTEN
BBRS_PORT
BBRS_DESTINATION
BBRS_TARGET
BBRS_INCLUDE
BBRS_IGNORE
BBRS_LOG_DIR
BBRS_VERBOSE
```

`BBRS_INCLUDE` and `BBRS_IGNORE` are comma-separated lists.

Example `/etc/bbrs/env` or `~/conf/bbrs/env`:

```env
BBRS_LISTEN=127.0.0.1
BBRS_PORT=12525
BBRS_DESTINATION=scripts
BBRS_TARGET=home
BBRS_INCLUDE=*.txt,*.ns
BBRS_IGNORE=vendor,tmp,*.map
BBRS_VERBOSE=false
```

## Config File

Optional settings can live in `<source>/.bbrs/config.toml`.

Example `config.toml`:

```toml
listen = "127.0.0.1"
port = 12525
destination = "scripts"
target = "home"
include = ["*.txt", "*.ns"]
ignore = ["vendor", "tmp,*.map"]
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

## Include Patterns

Default included files:

```text
*.js
*.ts
```

`*.d.ts` is always excluded.

`--include` expands the default include set. It does not replace defaults. Repeated and comma-separated patterns both work.

```sh
./bbrs -s ./src --include '*.txt'
./bbrs -s ./src --include '*.js,*.ts,*.ns'
./bbrs -s ./src --include '*.script' --include '*.txt'
```

Include patterns use Go `path.Match` shell-style glob rules and match slash-normalized relative paths and base filenames.

## Ignore Patterns

Default ignored paths:

```text
.bbrs
.git
target
node_modules
dist
build
.zed
.vscode
.idea
coverage
tmp
temp
```

`--ignore` expands the default ignore set. It does not replace defaults. Repeated and comma-separated patterns both work.

```sh
./bbrs -s ./src --ignore vendor
./bbrs -s ./src --ignore 'dist,tmp,*.map'
./bbrs -s ./src --ignore vendor --ignore '*.map'
```

Ignore patterns use Go `path.Match` shell-style glob rules and match slash-normalized relative paths and base filenames.

## Mirror Rules

Each sync:

1. Walks `--source`.
2. Skips ignored paths matching default ignore patterns plus any `--ignore` or `ignore` config values.
3. Uploads every desired local file to `--target` under `--destination`.
4. Fetches remote metadata with `getAllFileMetadata`.
5. Deletes stale remote files only when they are under `--destination` and match active include patterns.

Individual upload and delete failures are logged and counted in `failed`; other files still sync.

Remote paths are rejected when absolute, empty, using Windows drive prefixes, or containing `..`.

## Development

```sh
make test
make fmt
make vet
make tidy
```
