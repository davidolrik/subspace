# Configuration Reference

Complete reference for the Subspace KDL configuration format.

## Config File Location

The default path is `~/.config/subspace/config.kdl`. Override with the `--config` flag.

The config directory respects `$XDG_CONFIG_HOME`. If set, the default path becomes `$XDG_CONFIG_HOME/subspace/config.kdl`.

## Nodes

### `listen`

**Required.** The address to listen on.

```kdl
listen ":8118"
listen "127.0.0.1:8118"
listen "0.0.0.0:3128"
```

::: warning
Changing the listen address requires a restart — hot reload cannot rebind the listener.
:::

### `control_socket`

Path to the Unix domain socket for the control API. Used by the `status` and `logs` commands.

**Default:** `~/.config/subspace/control.sock`

```kdl
control_socket "/run/subspace/control.sock"
```

::: warning
Changing the control socket path requires a restart.
:::

### `upstream`

Defines a named upstream proxy.

```kdl
upstream "<name>" {
  type "<http|socks5>"
  address "<host:port>"
  username "<string>"   // optional
  password "<string>"   // optional
}
```

| Property | Required | Values | Description |
|---|---|---|---|
| `type` | Yes | `"http"`, `"socks5"` | Proxy protocol |
| `address` | Yes | `host:port` | Upstream proxy endpoint |
| `username` | No | string | Authentication username |
| `password` | No | string | Authentication password |

**HTTP type** uses the HTTP CONNECT method to establish tunnels. Supports Proxy-Authorization with Basic auth when credentials are provided.

**SOCKS5 type** uses the SOCKS5 protocol. Supports username/password authentication when credentials are provided.

### `route`

Maps a hostname pattern to an upstream.

```kdl
route "<pattern>" via="<upstream-name>"
```

The `via` property is required and must reference a defined upstream name, or the built-in `direct` upstream.

See [Pattern Matching](/reference/pattern-matching) for pattern syntax.

### `include`

Includes other KDL config files.

```kdl
include "<path-or-glob>"
```

| Behavior | Description |
|---|---|
| Path resolution | Relative to the file containing the include |
| Glob support | `*`, `?`, `[...]` via `filepath.Glob` |
| Ordering | Glob matches processed alphabetically |
| Nesting | Included files can include other files |
| Circular detection | Detected and rejected with an error |
| Missing glob | Silently ignored (empty expansion) |
| Missing exact path | Error |

## Validation

Config validation runs after all includes are resolved:

- All `upstream` blocks must have a valid `type` and `address`
- All `route` rules must reference an existing upstream or `"direct"`
- Circular includes are rejected
- Unknown node types produce an error

## Hot Reload

All parsed files (main config and all includes) are monitored via filesystem watchers. Changes trigger a full re-parse and validation cycle. If valid, the new routing is applied atomically. If invalid, the current config stays active and a warning is logged.

New files added to watched directories are also detected and trigger a reload.
