# Commands

## Global Flags

| Flag       | Description         | Default                         |
| ---------- | ------------------- | ------------------------------- |
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

| Flag         | Description     | Default |
| ------------ | --------------- | ------- |
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

## `subspace top <kind>`

Renders a ranked summary of activity from the persistent statistics database. Reads the same SQLite file that `serve` writes (`<config-dir>/stats.db`), so it works whether or not the proxy is currently running. Three kinds are supported:

- `upstreams` — ranked by per-upstream traffic.
- `domains` — ranked by destination hostname (extracted from Host header / SNI / SOCKS5 destination).
- `routes` — ranked by matched route pattern (`direct` for traffic that didn't match any rule).

```sh
subspace top upstreams                  # default: top 10 by bytes_total over 24h
subspace top domains -w 168h            # busiest hosts over the last 7 days
subspace top routes  -m success -n 5    # routes carrying the most successful conns
subspace top domains -m failures        # hosts that fail most often
subspace top upstreams -J               # JSON for piping into jq / a dashboard
```

| Flag             | Description                                                                    | Default        |
| ---------------- | ------------------------------------------------------------------------------ | -------------- |
| `-w, --window`   | Time window (`time.ParseDuration` syntax — `1h`, `24h`, `168h`)                | `24h`          |
| `-m, --metric`   | One of `success`, `failures`, `bytes_in`, `bytes_out`, `bytes_total`           | `bytes_total`  |
| `-n, --limit`    | Maximum number of entries returned                                              | `10`           |
| `-J, --json`     | Emit a JSON envelope (`{kind, metric, window, limit, top: [{name, value}]}`)   | `false`        |

Counters in the database are cumulative since process start, so the value shown is the per-window delta (`MAX − MIN` of the metric over the window). A subspace restart inside the window can cause a small undercount; for long windows that's negligible.

The same data is rendered live on the [statistics page](/guide/pages#statistics-page) under "Top Activity", with the metric selector controlling all three lists at once.

Example:

```text
  Top 2 upstreams by bytes_total over 24h0m0s

   1.  direct  2.1 GiB
   2.  hq      63.1 MiB
```

## `subspace schema`

Prints the embedded KDL schema describing every node, property, and child the config file accepts. Editors with kdl-schema support can use this for completion and validation; for editors without schema support it doubles as a machine-readable reference.

```sh
subspace schema > ~/.config/subspace/subspace.kdl-schema
```

Then point your editor at the file. Most KDL editor extensions look for a `kdl-schema` directive on the first line of the document, so add a comment to the top of your `config.kdl`:

```kdl
// kdl-schema "./subspace.kdl-schema"
```

The same schema is published at [`https://subspace.pub/subspace.kdl-schema`](https://subspace.pub/subspace.kdl-schema), so you can also reference it by URL if your editor supports remote schemas.

## `subspace validate`

Parses the config (main file plus all `include`s and `page` files) and reports any errors without starting the server. Useful for CI on a config repo, or as a pre-commit step.

```sh
subspace validate                         # uses --config or default location
subspace validate --config ./subspace.kdl # validate a specific file
```

Validation runs the same pipeline as `serve`:

- KDL syntax of the main config and every included/page file.
- Cross-references: routes pointing at undefined upstreams, fallbacks differing from `via`, default search engine pointing at a real engine, page tags pointing at the global `tags` block, etc.
- Page KDL parse errors (a broken page is reported and counts as a failure).

Output is a count summary plus either `OK` (exit `0`) or a numbered list of errors (exit `1`):

```text
  config          /home/me/.config/subspace/config.kdl
  files included   12
  upstreams        5
  routes           57
  pages            3
  tags             16
  search engines   8

  OK
```

A failing example:

```text
  1 error(s):
    • route "*.x" references unknown upstream "missing" (route dropped)
```

Wire it into CI as `subspace validate --config path/to/config.kdl`; the non-zero exit on failure is enough to fail the job.

## `subspace logs`

Streams log output from a running server via the control socket. Shows historical lines first, then optionally follows live output.

```sh
subspace logs                    # last 10 info+ lines
subspace logs -N 50              # last 50 lines
subspace logs -L error           # only errors
subspace logs -L debug -F        # all levels, follow live
```

| Flag           | Description                                         | Default |
| -------------- | --------------------------------------------------- | ------- |
| `-N, --lines`  | Number of historical lines to show                  | `10`    |
| `-L, --level`  | Minimum log level: `debug`, `info`, `warn`, `error` | `info`  |
| `-F, --follow` | Stream live output after showing history            | `false` |

## `subspace version`

Prints the version and exits.

```sh
subspace version
```
