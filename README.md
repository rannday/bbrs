# bbrs

`bbrs` mirrors a local source directory into Bitburner through the Bitburner Remote API websocket.

This tool is intentionally live and destructive. On each sync, it uploads matching local files, overwrites remote files, and deletes stale matching remote files under the configured destination.

## Build

```sh
go build -o bbrs ./cmd/bbrs
```

## Install Locally

```sh
go install ./cmd/bbrs
```

## Run

```sh
./bbrs -s ./src
./bbrs -s ./src -d scripts
./bbrs -s ./src --host home --pattern '*.txt'
./bbrs -s ./src -l 127.0.0.1 -p 12525
```

`bbrs` starts a websocket server. In Bitburner, open `Options -> Remote API`, set host `127.0.0.1` and port `12525`, then connect.

On first connection, `bbrs` performs a full sync. It keeps running and polls the source directory for changes. Saves, creates, and deletes trigger another full mirror sync.

## Options

```text
-h, --help
-s, --source       Local source directory to sync. Required.
-l, --listen       Listen address. Default: 127.0.0.1.
-p, --port         Listen port. Default: 12525.
-d, --destination  Destination directory inside Bitburner. Default: empty/root.
--host             Destination Bitburner host. Default: home.
--pattern          Additional filename patterns to include.
--logdir           Directory for log files.
-y, --yes          Skip destructive-operation confirmation.
```

On sync, `bbrs` logs `uploaded`, `skipped`, `deleted`, and `ignored` counts. Unchanged local files are skipped using a local upload cache, so repeated syncs only push files that changed since the last successful upload.

## Logging

`bbrs` uses [go-log](https://github.com/rannday/go-log) for structured console and file logging.

Default log location:

- Unix-like systems: `/var/log/bbrs/` when that directory already exists.
- Otherwise: `<source>/.bbrs/`
- Windows: `<source>/.bbrs/`

Each run writes to `bbrs_log_<timestamp>.log`. Logs rotate at 10 MiB with up to five backups.

Override with `--logdir`:

```sh
./bbrs -s ./src --logdir /tmp/bbrs-logs
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
2. Skips ignored directories: `.bbrs`, `.git`, `target`, `node_modules`, `dist`, `build`, `.zed`, `.vscode`, `.idea`, `coverage`, `tmp`, `temp`.
3. Uploads every desired local file to `--host` under `--destination`.
4. Fetches remote names with `getFileNames`.
5. Deletes stale remote files only when they are under `--destination` and match active patterns.

Remote paths are rejected when absolute, empty, using Windows drive prefixes, or containing `..`.
