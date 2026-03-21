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

## Hot Reload

All config files (main and included) are watched for changes. When any file is modified — or a new file is added to a watched directory — Subspace re-parses the entire config tree, validates it, and applies the new routing if valid.

Invalid configs are rejected with a warning log and the current routing stays active.

::: tip
Settings that require a restart to take effect: `listen` and `control_socket`. All other changes are applied live.
:::

## Complete Example

```kdl
listen ":8118"
control_socket "/tmp/subspace.sock"

// Upstream proxies
upstream "corporate" {
  type "http"
  address "proxy.corp.com:3128"
  username "user"
  password "pass"
}

upstream "tunnel" {
  type "socks5"
  address "127.0.0.1:1080"
}

// Routing rules (last match wins)
route ".corp.internal" via="corporate"
route "10.0.0.0/8" via="corporate"
route "192.168.*.*" via="corporate"
route "public.corp.internal" via="direct"
route "private.example.com" via="tunnel"
```
