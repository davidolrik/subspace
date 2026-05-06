# Configuration

Subspace uses [KDL](https://kdl.dev) as its configuration language. The default config path is `~/.config/subspace/config.kdl` (respects `$XDG_CONFIG_HOME`).

## `listen`

The address to listen on. Required.

```kdl
listen "127.0.0.1:8118"
```

Listen on all interfaces (use with caution):

```kdl
listen ":8118"
```

Subspace accepts HTTP, HTTPS, and SOCKS5 connections on the same port. The protocol is auto-detected from the first byte of each connection — no separate listeners or configuration needed.

## `control_socket`

Path to the Unix socket used by the `status` and `logs` commands to communicate with a running server. Defaults to `~/.config/subspace/control.sock`.

```kdl
control_socket "/tmp/subspace.sock"
```

## `upstream`

Defines a named upstream proxy.

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

| Property   | Required | Description                       |
| ---------- | -------- | --------------------------------- |
| `type`     | Yes      | `"http"`                          |
| `address`  | Yes      | `host:port` of the proxy          |
| `username` | No       | Authentication username           |
| `password` | No       | Authentication password           |

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

| Property   | Required | Description                       |
| ---------- | -------- | --------------------------------- |
| `type`     | Yes      | `"socks5"`                        |
| `address`  | Yes      | `host:port` of the proxy          |
| `username` | No       | Authentication username           |
| `password` | No       | Authentication password           |

### WireGuard

Routes traffic through a userspace WireGuard tunnel. Runs entirely in-process — no root privileges, kernel module, or external tools required. Generate keys with `wg genkey` and `wg pubkey` from [wireguard-tools](https://www.wireguard.com/install/).

```kdl
upstream "home" {
  type "wireguard"
  endpoint "vpn.example.com:51820"
  private-key "base64-encoded-private-key"
  public-key "base64-encoded-peer-public-key"
  address "10.0.0.2/32"
  dns "1.1.1.1"  // optional
}
```

| Property      | Required | Description                              |
| ------------- | -------- | ---------------------------------------- |
| `type`        | Yes      | `"wireguard"`                            |
| `endpoint`    | Yes      | `host:port` of the WireGuard peer        |
| `private-key` | Yes      | Base64-encoded private key               |
| `public-key`  | Yes      | Base64-encoded peer public key           |
| `address`     | Yes      | Local tunnel address with CIDR           |
| `dns`         | No       | DNS server for resolution via the tunnel |

## `route`

Routes traffic matching a pattern through a named upstream. See [Routing](/guide/routing) for full pattern syntax.

```kdl
route ".corp.internal"   via="corporate"
route "10.0.0.0/8"       via="internal"
route "bypass.example"   via="direct"
route ".doubleclick.net" via="blackhole"
```

Rules are evaluated in order. The **last matching rule wins**. Unmatched traffic connects directly.

Two reserved pseudo-upstreams need no `upstream` block:

- **`direct`** — bypass all proxying for a pattern. Useful for exempting specific hosts from a broader rule.
- **`blackhole`** — drop the traffic with a fast refusal (HTTP 451 / SOCKS5 0x02 / TLS close). Useful for blocking ad networks or telemetry endpoints. See [Routing → Blackhole](/guide/routing#blackhole) for details.

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

```text
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
listen "127.0.0.1:8118"
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

Defines an internal page served at `pages.subspace.pub/{name}/` when browsing through the proxy. Multiple `page` directives create multiple pages, each with its own path and menu entry.

```kdl
page "dev.kdl"
page "ops.kdl"
page "my-page.kdl" name="internal" alias="int"
```

By default, the page name is derived from the filename (minus the `.kdl` extension). Override it with `name=`, and add an alias with `alias=`.

| Config                            | URL                                                              |
| --------------------------------- | ---------------------------------------------------------------- |
| `page "dev.kdl"`                  | `http://pages.subspace.pub/dev/`                                 |
| `page "my-file.kdl" name="tools"` | `http://pages.subspace.pub/tools/`                              |
| `page "ops.kdl" alias="o"`        | `http://pages.subspace.pub/ops/` and `http://p.subspace.pub/o/` |

`p.subspace.pub` is a shorthand for `pages.subspace.pub`.

Each page is configured in its own KDL file with links, sections, icons, and optional descriptions. See [Internal Pages](/guide/pages) for the full page file format, search, statistics, and other features.

## Search Engines

The `/` search palette can route queries through external services (Google, Metacpan, GitHub, etc.) by declaring a `search-engines` block:

```kdl
search-engines default="google" {
    engine "google"   url="https://www.google.com/search?q={query}" icon="si-google" alias="g"
    engine "metacpan" url="https://metacpan.org/search?q={query}"   icon="fa-cube"   alias="cpan"
}
```

Type `cpan ojo` to search Metacpan, or let any unmatched query fall through to the default engine. See [Internal Pages → Search Engines](/guide/pages#search-engines) for keyword and Tab autocomplete behaviour, and the [`search-engines` reference](/reference/configuration#search-engines) for the full field list.

## Hot Reload

All config files (main and included) are watched for changes. When any file is modified — or a new file is added to a watched directory — Subspace re-parses the entire config tree, validates it, and applies the new routing if valid.

Invalid configs are rejected with a warning log and the current routing stays active.

::: tip
Settings that require a restart to take effect: `listen` and `control_socket`. All other changes are applied live.
:::

## Complete Example

```text
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
listen "127.0.0.1:8118"

include "upstreams/*.kdl"
include "routes/*.kdl"

// Internal pages
page "dev.kdl"
page "ops.kdl" alias="o"

// External search engines reachable from the `/` palette
search-engines default="google" {
    engine "google"   url="https://www.google.com/search?q={query}" icon="si-google" alias="g"
    engine "metacpan" url="https://metacpan.org/search?q={query}"   icon="fa-cube"   alias="cpan"
}
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

- `http://pages.subspace.pub/dev/` — development links page
- `http://pages.subspace.pub/ops/` (or `http://p.subspace.pub/o/`) — operations links page
- `http://stats.subspace.pub/` — statistics dashboard
- `https://subspace.pub/` — documentation site
