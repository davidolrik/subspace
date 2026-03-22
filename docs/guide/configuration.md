# Configuration

Subspace uses [KDL](https://kdl.dev) as its configuration language. The default config path is `~/.config/subspace/config.kdl` (respects `$XDG_CONFIG_HOME`).

## `listen`

The address to listen on. Required.

```kdl
listen ":8118"
```

Bind to a specific interface:

```kdl
listen "127.0.0.1:8118"
```

## `control_socket`

Path to the Unix socket used by the `status` and `logs` commands to communicate with a running server. Defaults to `~/.config/subspace/control.sock`.

```kdl
control_socket "/tmp/subspace.sock"
```

## `upstream`

Defines a named upstream proxy. Each upstream has a type, address, and optional authentication.

### HTTP CONNECT

Tunnels through an HTTP proxy using the CONNECT method.

```kdl
upstream "corporate" {
  type "http"
  address "proxy.corp.com:3128"
  username "user"    // optional
  password "secret"  // optional
}
```

### SOCKS5

Tunnels through a SOCKS5 proxy.

```kdl
upstream "tunnel" {
  type "socks5"
  address "127.0.0.1:1080"
  username "user"    // optional
  password "secret"  // optional
}
```

### Properties

| Property | Required | Description |
|---|---|---|
| `type` | Yes | `"http"` or `"socks5"` |
| `address` | Yes | `host:port` of the upstream proxy |
| `username` | No | Authentication username |
| `password` | No | Authentication password |

## `route`

Routes traffic matching a pattern through a named upstream. See [Routing](/guide/routing) for full pattern syntax.

```kdl
route ".corp.internal" via="corporate"
route "10.0.0.0/8" via="internal"
route "bypass.example.com" via="direct"
```

Rules are evaluated in order. The **last matching rule wins**. Unmatched traffic connects directly.

The built-in upstream `direct` bypasses all proxying — useful for exempting specific hosts from a broader rule.

## `include`

Includes other config files. Paths are resolved relative to the file containing the include statement.

```kdl
include "upstreams/*.kdl"
include "routes/corporate.kdl"
```

Glob patterns are supported via standard shell glob syntax (`*`, `?`, `[...]`). Matches are processed in alphabetical order.

- Nested includes are supported (included files can include other files)
- Circular includes are detected and rejected
- Glob patterns matching no files are silently ignored
- Exact paths that don't exist produce an error

### Example: Split Config

```
~/.config/subspace/
├── config.kdl           # main config
├── dashboard.kdl        # link page
├── upstreams/
│   ├── corporate.kdl    # corporate proxy definition
│   └── tunnel.kdl       # SOCKS5 tunnel definition
└── routes/
    ├── corporate.kdl    # corporate routing rules
    └── personal.kdl     # personal routing rules
```

`config.kdl`:
```kdl
listen ":8118"
include "upstreams/*.kdl"
include "routes/*.kdl"
page "dashboard.kdl" alias="dash"
```

`upstreams/corporate.kdl`:
```kdl
upstream "corporate" {
  type "http"
  address "proxy.corp.com:3128"
}
```

`routes/corporate.kdl`:
```kdl
route ".corp.internal" via="corporate"
route ".corp.com" via="corporate"
```

## `page`

Defines an internal page served at `{name}.subspace` when browsing through the proxy. Multiple `page` directives create multiple pages, each with its own hostname and menu entry.

```kdl
page "dev.kdl"
page "ops.kdl"
page "my-page.kdl" host="internal" alias="int"
```

### Hostname

By default, the hostname is derived from the filename (minus the `.kdl` extension). Override it with `host=`:

| Config | URL |
|---|---|
| `page "dev.kdl"` | `http://dev.subspace/` |
| `page "my-file.kdl" host="tools"` | `http://tools.subspace/` |
| `page "ops.kdl" alias="o"` | `http://ops.subspace/` and `http://o.subspace/` |

The hostnames `stats` and `statistics` are reserved for the built-in statistics page and cannot be used.

### Page file format

Each page file is a KDL document with optional `title` and `footer`, and one or more `list` sections:

```kdl
title "Development Tools"
footer "Acme Corp — Internal Use Only"

list "Repositories" {
    link "GitHub" url="https://github.com/org" icon="si-github" description="Source code"
    link "GitLab" url="https://gitlab.corp.com" icon="si-gitlab"
}

list "Monitoring" {
    link "Grafana" url="https://grafana.example.com" icon="si-grafana" description="Dashboards"
    link "PagerDuty" url="https://pagerduty.com" icon="fa-bell"
}
```

### Link properties

| Property | Required | Description |
|---|---|---|
| `url` | Yes | The link URL |
| `icon` | No | Icon name — `si-*` for [Simple Icons](https://simpleicons.org), `fa-*` for [Font Awesome](https://fontawesome.com/icons) |
| `description` | No | Short description shown below the link name |

### Built-in pages

- **Statistics** — always available at `statistics.subspace` (or `stats.subspace`). Shows live metrics (connections, active, upstream health), and historical charts (connections over time, traffic by upstream, protocol breakdown). Statistics are persisted to a SQLite database at `~/.config/subspace/stats.db` and retained for one year with automatic downsampling (5s → 1m after 1 hour, 1m → 1h after 7 days).
- **Entry point** — navigating to `http://subspace.dk/` redirects to the first configured page, or to statistics if no pages are defined. When subspace is not running, `subspace.dk` resolves to a real web server that redirects to the [troubleshooting guide](/guide/troubleshooting#not-running).
- **Error pages** — DNS failures and connection errors show styled error pages instead of bare HTTP 502 responses.

All configured pages and the statistics page appear in a shared navigation menu. Icons are embedded in the binary — no external requests are made when viewing pages.

## Hot Reload

All config files (main and included) are watched for changes. When any file is modified — or a new file is added to a watched directory — Subspace re-parses the entire config tree, validates it, and applies the new routing if valid.

Invalid configs are rejected with a warning log and the current routing stays active.

::: tip
Settings that require a restart to take effect: `listen` and `control_socket`. All other changes are applied live.
:::

## Complete Example

```
~/.config/subspace/
├── config.kdl
├── dev.kdl
├── ops.kdl
├── upstreams/
│   └── corporate.kdl
└── routes/
    └── corporate.kdl
```

`config.kdl`:
```kdl
listen ":8118"

include "upstreams/*.kdl"
include "routes/*.kdl"

// Internal pages
page "dev.kdl"
page "ops.kdl" alias="o"
```

`dev.kdl`:
```kdl
title "Development"

list "Repositories" {
    link "GitHub" url="https://github.com/org" icon="si-github" description="Source code"
    link "CI/CD" url="https://ci.example.com" icon="fa-rocket"
}

list "Docs" {
    link "Wiki" url="https://wiki.example.com" icon="fa-book"
}
```

`ops.kdl`:
```kdl
title "Operations"
footer "On-call: #ops-oncall"

list "Monitoring" {
    link "Grafana" url="https://grafana.example.com" icon="si-grafana"
    link "PagerDuty" url="https://pagerduty.com" icon="fa-bell"
}
```

This gives you:
- `http://dev.subspace/` — development links page
- `http://ops.subspace/` (or `http://o.subspace/`) — operations links page
- `http://stats.subspace/` — statistics dashboard
- `http://subspace.dk/` — redirects to `dev.subspace` (first configured page)
