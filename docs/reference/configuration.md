# Configuration Reference

Complete reference for the Subspace KDL configuration format.

## Config File Location

The default path is `~/.config/subspace/config.kdl`. Override with the `--config` flag.

The config directory respects `$XDG_CONFIG_HOME`. If set, the default path becomes `$XDG_CONFIG_HOME/subspace/config.kdl`.

## Nodes

### `listen`

**Required.** The address to listen on. HTTP, HTTPS, and SOCKS5 connections are all accepted on the same port — the protocol is auto-detected from the first byte of each connection.

```kdl
listen "127.0.0.1:8118"
listen ":8118"
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

Defines a named upstream proxy. Supported types: `http`, `socks5`, `wireguard`.

#### HTTP CONNECT

Tunnels through an HTTP proxy using the CONNECT method. Supports Proxy-Authorization with Basic auth when credentials are provided.

```kdl
upstream "<name>" {
  type "http"
  address "<host:port>"
  username "<string>"   // optional
  password "<string>"   // optional
}
```

| Property   | Required | Description                  |
| ---------- | -------- | ---------------------------- |
| `type`     | Yes      | `"http"`                     |
| `address`  | Yes      | Proxy endpoint (`host:port`) |
| `username` | No       | Authentication username      |
| `password` | No       | Authentication password      |

#### SOCKS5

Tunnels through a SOCKS5 proxy. Supports username/password authentication when credentials are provided.

```kdl
upstream "<name>" {
  type "socks5"
  address "<host:port>"
  username "<string>"   // optional
  password "<string>"   // optional
}
```

| Property   | Required | Description                  |
| ---------- | -------- | ---------------------------- |
| `type`     | Yes      | `"socks5"`                   |
| `address`  | Yes      | Proxy endpoint (`host:port`) |
| `username` | No       | Authentication username      |
| `password` | No       | Authentication password      |

#### WireGuard

Creates a userspace WireGuard tunnel using [wireguard-go](https://git.zx2c4.com/wireguard-go/about/) and gVisor's netstack. Runs entirely in-process — no root privileges or kernel module required. Health checks are not performed for WireGuard upstreams (the protocol has its own keepalive mechanism).

```kdl
upstream "<name>" {
  type "wireguard"
  endpoint "<host:port>"
  private-key "<base64>"
  public-key "<base64>"
  address "<ip/cidr>"
  dns "<ip>"            // optional
}
```

| Property      | Required | Description                                         |
| ------------- | -------- | --------------------------------------------------- |
| `type`        | Yes      | `"wireguard"`                                       |
| `endpoint`    | Yes      | Peer endpoint (`host:port`)                         |
| `private-key` | Yes      | Base64-encoded local private key                    |
| `public-key`  | Yes      | Base64-encoded peer public key                      |
| `address`     | Yes      | Local tunnel address with CIDR (e.g. `10.0.0.2/32`) |
| `dns`         | No       | DNS server for resolution via the tunnel            |

### `route`

Maps a hostname pattern to an upstream.

```kdl
route "<pattern>" via="<upstream-name>"
```

The `via` property is required and must reference a defined upstream name, or the built-in `direct` upstream.

See [Pattern Matching](/reference/pattern-matching) for pattern syntax.

### `tags`

Defines a global palette of tags. Each tag has a name and a color and is rendered as a small pill in the page UI when referenced from a link or list section in a page KDL file.

```kdl
tags {
  tag "<name>" color="<#hex>" alias="<display>"
}
```

| Property | Required | Description                                                                                       |
| -------- | -------- | ------------------------------------------------------------------------------------------------- |
| `color`  | Yes      | Any valid CSS color (typically hex)                                                               |
| `alias`  | No       | Text shown on the pill. Defaults to the tag name. May repeat across tags to share a display label |

Tag names must be unique within the block. Aliases may repeat — use this to render multiple distinct tags (each with its own color) under the same display label, e.g. two `services` pills in different colors. Pages that reference an undefined tag fail validation at startup. See [Internal Pages → Tags](/guide/pages#tags) for usage.

### `search-engines`

Defines external search engines that the `/` palette can route queries to. The first token of a query is matched (case-insensitively) against engine names and aliases; on a hit, a top-of-list row routes the rest of the query through the engine.

```kdl
search-engines default="<name>" {
  engine "<name>" url="<https://...{query}...>" alias="<keyword>" icon="<icon>" description="<text>"
}
```

| Property      | Required | Description                                                                                                                                              |
| ------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`        | Yes      | Positional argument. Primary keyword used to invoke the engine. Must be unique (case-insensitive — `Google` and `google` collide).                      |
| `url`         | Yes      | Engine URL template. **Must contain the literal `{query}` placeholder.** Every occurrence is replaced with `encodeURIComponent(query)` at navigation time. |
| `alias`       | No       | Additional keyword that triggers the same engine.                                                                                                        |
| `icon`        | No       | Same icon system as links: `si-*`, `fa-*`, `mdi-*`, `nf-*`. Falls back to a magnifier icon when omitted.                                                |
| `description` | No       | Short text shown as the third line of the engine's result row in the search palette.                                                                    |
| `fallback`    | No       | When `#true`, the engine appears in the no-match fallback list alongside the default engine. Defaults to `#false` so engines stay keyword-only.         |

The block-level `default=` property names the engine shown first in the no-match fallback list. Additional engines appear in the same list when declared with `fallback=#true`, alphabetised by name. Engines without `fallback` (and not the default) stay keyword-only. The default reference is case-insensitive and must point at an engine declared in the same block — an unknown reference is downgraded to a non-fatal config error and the field is cleared. With no `default=` and no `fallback=#true` engines, queries with no matches produce empty results.

Engine names are stored case-insensitively (so duplicates and the default reference are matched without regard to case), but the original casing is preserved on the search palette row label. Engines hot-reload alongside the rest of the config; open dashboard tabs automatically reload within a few seconds. See [Internal Pages → Search Engines](/guide/pages#search-engines) for usage.

### `include`

Includes other KDL config files.

```kdl
include "<path-or-glob>"
```

| Behavior           | Description                                 |
| ------------------ | ------------------------------------------- |
| Path resolution    | Relative to the file containing the include |
| Glob support       | `*`, `?`, `[...]` via `filepath.Glob`       |
| Ordering           | Glob matches processed alphabetically       |
| Nesting            | Included files can include other files      |
| Circular detection | Detected and rejected with an error         |
| Missing glob       | Silently ignored (empty expansion)          |
| Missing exact path | Error                                       |

## Validation

Config validation runs after all includes are resolved:

- All `upstream` blocks must have a valid `type` and the required properties for that type
- All `route` rules must reference an existing upstream or `"direct"`
- Circular includes are rejected
- Unknown node types produce an error

## Hot Reload

All parsed files (main config and all includes) are monitored via filesystem watchers. Changes trigger a full re-parse and validation cycle. If valid, the new routing is applied atomically. If invalid, the current config stays active and a warning is logged.

New files added to watched directories are also detected and trigger a reload.
