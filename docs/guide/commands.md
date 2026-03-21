# Commands

## Global Flags

| Flag | Description | Default |
|---|---|---|
| `--config` | Path to config file | `~/.config/subspace/config.kdl` |

## `subspace serve`

Starts the proxy server. Loads the config, starts listening for connections, and watches config files for hot-reload.

```sh
subspace serve
subspace serve --config /path/to/config.kdl
```

The server logs its version, listening address, and upstream/route counts on startup. It shuts down gracefully on `SIGINT` or `SIGTERM`.

## `subspace status`

Shows health and status of all upstream proxies. Connects to the control socket of a running server.

```sh
subspace status
subspace status -J
```

For each upstream, shows:
- **Health** — TCP connectivity check (OK / FAIL)
- **Type and address** — proxy type and endpoint
- **Latency** — health check round-trip time
- **Traffic stats** — success/failure counts, bytes transferred

Also shows total and active connection counts, and connection pool statistics.

| Flag | Description | Default |
|---|---|---|
| `-J, --json` | Output raw JSON | `false` |

The built-in `direct` upstream is always listed at the bottom with its traffic stats.

## `subspace resolve <url>`

Shows which route and upstream would handle a given URL. Useful for debugging routing rules without sending actual traffic.

```sh
subspace resolve https://app.corp.internal/api
subspace resolve http://10.0.1.5:8080/health
subspace resolve example.com
```

Accepts a full URL, a `host:port`, or a bare hostname.

Shows:
- The extracted hostname
- The matching route pattern (if any)
- The selected upstream with type and address
- `direct connection` if no route matches

## `subspace logs`

Streams log output from a running server via the control socket. Shows historical lines first, then optionally follows live output.

```sh
subspace logs                    # last 10 info+ lines
subspace logs -N 50              # last 50 lines
subspace logs -L error           # only errors
subspace logs -L debug -F        # all levels, follow live
```

| Flag | Description | Default |
|---|---|---|
| `-N, --lines` | Number of historical lines to show | `10` |
| `-L, --level` | Minimum log level: `debug`, `info`, `warn`, `error` | `info` |
| `-F, --follow` | Stream live output after showing history | `false` |
