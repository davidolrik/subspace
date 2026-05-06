# Subspace

A transparent proxy that routes traffic through upstream proxies based on hostname and IP matching. Supports HTTP, HTTPS, WebSocket, WSS, and SOCKS5 without terminating TLS.

## Features

- **Transparent proxying** — HTTP, HTTPS, WebSocket, and WSS
- **SOCKS5 inbound** — accepts SOCKS5 clients on the same port, auto-detected alongside HTTP
- **No TLS termination** — extracts SNI from the ClientHello for routing, then tunnels raw bytes
- **HTTP CONNECT** — works as an explicit proxy for clients that support it
- **Flexible routing** — exact hostnames, domain suffixes, glob patterns, and CIDR subnets
- **Upstream proxy support** — HTTP CONNECT, SOCKS5, and WireGuard upstreams

- **Connection pooling** — reuses upstream connections across HTTP requests
- **HTTP keep-alive** — serves multiple requests per client connection
- **Hot reload** — config changes (including included files) are detected and applied without restart
- **Config includes** — split config across multiple files with glob support
- **Live log streaming** — stream colored logs from a running server with level filtering
- **Health checks** — TCP health checks for all upstreams via the status command
- **Internal pages** — link dashboards at `pages.subspace.pub` and statistics at `stats.subspace.pub`
- **Statistics** — live metrics, upstream health, and historical charts with persistent SQLite storage
- **Styled error pages** — DNS failures and connection errors show helpful error pages instead of bare 502s

## Installation

### Homebrew (macOS)

```sh
brew install davidolrik/tap/subspace
```

To run subspace as a background service that starts automatically at login:

```sh
brew services start subspace
```

### From Source

Requires Go 1.26 or later.

```sh
go install go.olrik.dev/subspace@latest
```

Or clone and build:

```sh
git clone https://github.com/davidolrik/subspace.git
cd subspace
go build -o subspace .
```

## Quick Start

Create a config file at `~/.config/subspace/config.kdl`:

```kdl
listen "127.0.0.1:8118"

upstream "corporate" {
  type "http"
  address "proxy.corp.com:3128"
  username "user"
  password "pass"
}

upstream "tunnel" {
  type "socks5"
  address "socks.example.com:1080"
}

route ".corp.internal" via="corporate"
route "specific.host.com" via="tunnel"
```

Start the proxy:

```sh
subspace serve
```

Use it:

```sh
# HTTP proxy
curl -x http://localhost:8118 http://example.com

# HTTPS (via CONNECT)
curl -x http://localhost:8118 https://example.com

# SOCKS5 (same port)
curl --socks5-hostname localhost:8118 https://example.com
git -c http.proxy=socks5h://localhost:8118 clone https://github.com/org/repo
```

## Configuration

Subspace uses [KDL](https://kdl.dev) for configuration. The default config path is `~/.config/subspace/config.kdl` (respects `$XDG_CONFIG_HOME`).

### `listen`

The address to listen on.

```kdl
listen "127.0.0.1:8118"
```

### `control_socket`

Path to the Unix control socket used by `status` and `logs` commands. Defaults to `~/.config/subspace/control.sock`.

```kdl
control_socket "/tmp/subspace.sock"
```

### `upstream`

Defines a named upstream proxy. Supported types: `http` (HTTP CONNECT), `socks5`, and `wireguard`.

```kdl
upstream "corporate" {
  type "http"
  address "proxy.corp.com:3128"
  username "user"    // optional
  password "secret"  // optional
}

upstream "tunnel" {
  type "socks5"
  address "127.0.0.1:1080"
}

upstream "home" {
  type "wireguard"
  endpoint "vpn.example.com:51820"
  private-key "base64-encoded-private-key"
  public-key "base64-encoded-peer-public-key"
  address "10.0.0.2/32"
  dns "1.1.1.1"  // optional
}
```

### `route`

Routes traffic matching a pattern through a named upstream. Rules are evaluated in order and the last match wins.

```kdl
// Exact hostname
route "specific.host.com" via="myproxy"

// Domain suffix — matches any subdomain of example.com
route ".example.com" via="myproxy"

// Glob pattern — matches IP ranges
route "192.168.*.*" via="lan-proxy"

// CIDR subnet — matches IP addresses in the network
route "10.0.0.0/8" via="internal"

// Direct — bypass all upstreams for this pattern
route "bypass.corp.com" via="direct"

// Blackhole — drop traffic for this pattern (refuses with HTTP 451 / SOCKS5 0x02)
route ".ads.example.com" via="blackhole"
route "*.telemetry.com"  via="blackhole"

// Fallback to blackhole — if the work proxy is down, refuse rather than leak directly
route ".risky.example" via="corporate" fallback="blackhole"
```

Pattern types:

| Pattern       | Example          | Matches                                           |
| ------------- | ---------------- | ------------------------------------------------- |
| Exact         | `"example.com"`  | `example.com` only                                |
| Domain suffix | `".example.com"` | `foo.example.com`, `bar.baz.example.com`          |
| Glob          | `"192.168.*.*"`  | `192.168.1.1`, `192.168.0.255`                    |
| CIDR          | `"10.0.0.0/8"`   | any IP in the `10.0.0.0/8` subnet (IPv4 and IPv6) |

Two built-in pseudo-upstreams need no `upstream` block:

- `direct` — bypass any broader matching upstream and connect straight to the target.
- `blackhole` — drop the traffic. HTTP/CONNECT/WebSocket clients receive a synthetic
  `HTTP/1.1 451 Unavailable For Legal Reasons`, SOCKS5 clients get reply byte `0x02`
  (connection not allowed by ruleset), and TLS pass-through connections are closed.
  Drops are tracked per-route, per-domain and per-upstream in `subspace status` and
  the statistics dashboard so you can see how much traffic was prevented from leaving.

Unmatched traffic always connects directly.

### `include`

Includes other config files, resolved relative to the file containing the include statement. Supports glob patterns.

```kdl
include "upstreams/*.kdl"
include "routes/corporate.kdl"
```

Nested includes are supported. Circular includes are detected and rejected. Glob patterns that match no files are silently ignored; exact paths that don't exist produce an error.

New files added to an already-included directory are picked up automatically on the next config change.

### `page`

Defines an internal page served at `pages.subspace.pub/{name}/`. The page name is derived from the filename by default, or set explicitly with `name=`. An optional `alias=` adds a second path. `p.subspace.pub` is a shorthand for `pages.subspace.pub`.

```kdl
page "dev.kdl"
page "ops.kdl"
page "my-page.kdl" name="internal" alias="int"
```

This creates pages at `pages.subspace.pub/dev/`, `pages.subspace.pub/ops/`, and `pages.subspace.pub/internal/` (also `p.subspace.pub/int/`). Each page is configured in its own KDL file:

```kdl
title "Development"
footer "Acme Corp"

list "Repositories" {
    link "GitHub" url="https://github.com/org" icon="si-github" description="Source code"
    link "CI/CD" url="https://ci.example.com" icon="fa-rocket"
}
```

Icons are embedded in the binary (no external requests). Use `si-*` for [Simple Icons](https://simpleicons.org) and `fa-*` for [Font Awesome](https://fontawesome.com/icons).

All configured pages and the statistics page appear in a shared navigation menu.

### Built-in Pages

- **Statistics** — always available at `stats.subspace.pub` (or `statistics.subspace.pub`). Shows live metrics (connections, active, upstream health), and historical charts (connections over time, traffic by upstream, protocol breakdown). Statistics are persisted to a SQLite database and retained for one year with automatic downsampling.
- **Fallback** — when subspace is not running, an external redirect server sends visitors to the documentation site at `https://subspace.pub/`.
- **Error pages** — DNS failures and connection errors show styled error pages instead of bare HTTP 502 responses.

The statistics page is always available at `stats.subspace.pub` regardless of page configuration. `p.subspace.pub` is a shorthand for `pages.subspace.pub`.

### Hot Reload

All config files (main and included) are watched for changes. When any file is modified, Subspace re-parses the entire config tree, validates it, and applies the new routing if valid. Invalid configs are rejected with a warning and the current routing stays active.

Settings that require a restart: `listen`, `control_socket`.

## Commands

### `subspace serve`

Starts the proxy server.

```sh
subspace serve
subspace serve --config /path/to/config.kdl
```

### `subspace status`

Shows health and status of upstream proxies. Performs a TCP health check against each upstream.

```sh
subspace status       # colored terminal output
subspace status -J    # raw JSON output
```

### `subspace resolve <url>`

Shows which route and upstream would handle a given URL.

```sh
$ subspace resolve https://app.corp.internal/api
```

### `subspace logs`

Streams logs from a running server.

```sh
subspace logs                    # last 10 info+ lines
subspace logs -N 50              # last 50 lines
subspace logs -L error           # only errors
subspace logs -L debug -F        # all levels, follow live
```

| Flag           | Description                                     | Default |
| -------------- | ----------------------------------------------- | ------- |
| `-N, --lines`  | Number of historical lines                      | `10`    |
| `-L, --level`  | Minimum level: `debug`, `info`, `warn`, `error` | `info`  |
| `-F, --follow` | Stream live output after history                | `false` |

### `subspace version`

Prints the version and exits.

```sh
subspace version
```

## How It Works

### Connection Classification

When a connection arrives, Subspace peeks at the first byte to classify the protocol:

- **`0x05` (SOCKS5)** — Performs the SOCKS5 handshake to extract the target hostname and port. Routes through the same upstream selection as HTTP CONNECT, then relays bidirectionally.
- **`0x16` (TLS)** — Parses the ClientHello to extract the SNI hostname, reads the full record length for reliable extraction. Routes based on SNI, then tunnels the raw TLS bytes without decryption.
- **HTTP CONNECT** — Responds with `200 Connection Established` and relays bytes bidirectionally.
- **Plain HTTP** — Reads the `Host` header for routing. Detects `Upgrade: websocket` for WebSocket upgrade. Supports HTTP/1.1 keep-alive for multiple requests per connection.

All protocols share the same routing rules and upstream configuration. SOCKS5 clients (git, ssh, curl) can use the same port as HTTP proxy clients.

### Connection Pooling

HTTP requests reuse upstream connections when possible. After a response is fully read, the upstream connection is returned to a per-host pool instead of being closed. The next request to the same upstream and target address gets the pooled connection, avoiding a fresh TCP + proxy handshake.

Pool defaults: 4 idle connections per host, 90 second idle timeout. The pool is drained on config reload.

CONNECT and TLS connections use bidirectional relay and are not pooled.

### Zero-Copy Relay

For tunneled connections (TLS, CONNECT, WebSocket), Subspace unwraps the buffered reader before entering the relay loop. This allows the kernel to use splice/sendfile for zero-copy data transfer between sockets.

## Environment

Subspace clears proxy-related environment variables (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `ALL_PROXY`) on startup. Routing is controlled exclusively by the config file.

Respects `$XDG_CONFIG_HOME` for the default config directory. Respects `$NO_COLOR` to disable colored output.

## License

MIT
