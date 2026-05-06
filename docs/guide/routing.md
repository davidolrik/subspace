# Routing

Subspace routes connections based on hostname or IP address patterns. Rules are evaluated in order, and the **last matching rule wins**. If no rule matches, the connection goes direct.

## Pattern Types

### Exact Match

Matches a single hostname exactly.

```kdl
route "api.example.com" via="myproxy"
```

Matches `api.example.com` only. Does not match `www.api.example.com` or `example.com`.

### Domain Suffix

Matches any subdomain of a domain. The pattern starts with a dot.

```kdl
route ".example.com" via="myproxy"
```

Matches `foo.example.com`, `bar.baz.example.com`. Does **not** match `example.com` itself.

### Glob

Matches using shell-style glob patterns with `*` and `?` wildcards.

```kdl
route "192.168.*.*" via="lan-proxy"
route "*.cdn.example.com" via="cdn-proxy"
```

`*` matches any sequence of non-dot characters. `?` matches a single character. This uses Go's `filepath.Match` semantics.

### CIDR

Matches IP addresses within a network range. Supports both IPv4 and IPv6.

```kdl
route "10.0.0.0/8" via="internal"
route "172.16.0.0/12" via="internal"
route "192.168.0.0/16" via="internal"
route "fd00::/8" via="internal6"
```

The host is parsed as an IP address and checked against the CIDR range. Non-IP hostnames never match CIDR rules.

### Catch-All

Use `.` (the DNS root) or `*` to match every host. This is useful for sending all traffic through a single upstream and then carving out exceptions with more specific rules.

```kdl
route "." via="hq"
```

`.` and `*` are equivalent here — pick whichever reads better in your config.

### Direct

The built-in upstream `direct` bypasses all proxying. Use it to exempt specific hosts from a broader rule.

```kdl
// Route all of .corp.com through the proxy...
route ".corp.com" via="corporate"

// ...except the public site, which goes direct
route "public.corp.com" via="direct"
```

### Blackhole

The built-in upstream `blackhole` drops the connection without dialing anywhere. No `upstream` block is required — `blackhole` and `direct` are reserved built-in names. Use it to block ad networks, telemetry endpoints, or anything else you'd rather not let your machine talk to.

```kdl
route ".doubleclick.net" via="blackhole"
route "*.telemetry.example" via="blackhole"
route "10.66.0.0/16" via="blackhole"
```

#### Refusal behaviour per protocol

The proxy refuses immediately — there's no slow timeout, no failed-DNS error, no leaked connection. The wire format depends on how the client reached the proxy:

| Client protocol             | Refusal                                                                |
| --------------------------- | ---------------------------------------------------------------------- |
| HTTP (plain)                | `HTTP/1.1 451 Unavailable For Legal Reasons` with a styled error page  |
| HTTP CONNECT                | `HTTP/1.1 451 Unavailable For Legal Reasons` then close                |
| WebSocket upgrade           | `HTTP/1.1 451 Unavailable For Legal Reasons` (the upgrade is rejected) |
| SOCKS5                      | Reply byte `0x02` — *connection not allowed by ruleset* (RFC 1928)     |
| Transparent TLS (SNI-based) | Connection closed (no application-layer channel before the handshake)  |

[HTTP 451](https://datatracker.ietf.org/doc/html/rfc7725) was chosen over `403` and `502` because it specifically signals "this resource is being refused on purpose," not "the server failed" or "you're not authorised." Browsers won't auto-retry, and the styled error page tells the user what happened.

#### Use as a fallback

`blackhole` works wherever a `via=` would — including in the `fallback=` slot. This is useful when you'd rather drop traffic than leak it directly if the work proxy goes down:

```kdl
// If "corporate" is unhealthy, refuse rather than connect directly.
route ".corp.internal" via="corporate" fallback="blackhole"
```

The blackhole short-circuit happens after the primary upstream's dial fails or its health check fails — no extra dial attempts, no slow timeouts.

#### Catch-all blocking

Use `via="blackhole"` with a catch-all pattern to flip subspace into "deny by default" mode and explicitly allow only the routes you've defined:

```kdl
// Allow specific destinations through their named upstreams...
route ".corp.internal" via="corporate"
route ".internal.lan"  via="home-vpn"

// ...and drop everything else.
route "." via="blackhole"
```

Because the last matching rule wins, the broad `route "."` only applies when nothing more specific does.

#### Stats and visibility

Drops are not silent — every blackhole is recorded:

- **`subspace status`** — `blackhole` appears in the upstreams table alongside your declared upstreams (sorted at the bottom with `direct`), with a running count of drops, the bytes-in clients tried to send, and the bytes-out of the synthetic refusals.
- **`subspace top upstreams|domains|routes`** — blackhole hits show up in the top-N rankings, so you can see *which* hosts are being blocked the most.
- **Statistics dashboard** — the "Traffic by Upstream" chart and "Top Activity" panels include a `blackhole` series.
- **`subspace resolve <url>`** — confirms a URL routes to blackhole and notes the refusal mode.
- **Logs** — every drop emits a `blackhole refused` debug log with the matched pattern and host. Visible with `subspace logs -L debug`.

#### Reserved name

You can't define an upstream named `blackhole` (or `direct`) — those names are reserved for the built-ins. Subspace will emit a config error and skip the offending block.

## Rule Ordering

Rules are evaluated in order. The **last matching rule wins**. This lets you set broad rules first and override with specific exceptions later.

```kdl
// Broad rule: all internal traffic through corporate
route ".corp.internal" via="corporate"
route "10.0.0.0/8" via="corporate"

// Exception: this specific host goes through the tunnel instead
route "secret.corp.internal" via="tunnel"

// Exception: this subnet goes direct
route "10.99.0.0/16" via="direct"
```

In this example, `secret.corp.internal` matches both `.corp.internal` and the exact rule — the exact rule wins because it comes last.

## Matching Behavior

- **Port stripping** — hostnames with ports (e.g. `example.com:8080`) have the port stripped before matching
- **Case insensitive** — all matching is case-insensitive
- **Patterns are pre-compiled** — CIDR networks are parsed once at config load, not per-request

## Pattern Detection

Subspace determines the pattern type automatically:

| Contains             | Interpreted as         |
| -------------------- | ---------------------- |
| `/` (and valid CIDR) | CIDR subnet            |
| `*` or `?`           | Glob pattern           |
| Exactly `.`          | Catch-all (every host) |
| Leading `.`          | Domain suffix          |
| Otherwise            | Exact match            |
