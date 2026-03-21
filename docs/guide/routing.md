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

### Direct

The built-in upstream `direct` bypasses all proxying. Use it to exempt specific hosts from a broader rule.

```kdl
// Route all of .corp.com through the proxy...
route ".corp.com" via="corporate"

// ...except the public site, which goes direct
route "public.corp.com" via="direct"
```

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

| Contains | Interpreted as |
|---|---|
| `/` (and valid CIDR) | CIDR subnet |
| `*` or `?` | Glob pattern |
| Leading `.` | Domain suffix |
| Otherwise | Exact match |
