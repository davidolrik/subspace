# Pattern Matching Reference

Route patterns are pre-compiled at config load time. The pattern type is detected automatically.

## Pattern Types

### Exact

No special characters. Matches the hostname exactly (case-insensitive).

```kdl
route "api.example.com" via="proxy"
```

### Domain Suffix

Starts with `.`. Matches any host ending with the suffix.

```kdl
route ".example.com" via="proxy"
```

| Input | Matches? |
|---|---|
| `foo.example.com` | Yes |
| `bar.baz.example.com` | Yes |
| `example.com` | No |
| `notexample.com` | No |

### Glob

Contains `*` or `?`. Uses `filepath.Match` semantics.

```kdl
route "192.168.*.*" via="proxy"
route "*.cdn.example.com" via="cdn"
route "test?.example.com" via="proxy"
```

| Wildcard | Matches |
|---|---|
| `*` | Any sequence of non-separator characters |
| `?` | Any single non-separator character |

::: tip
The separator is `.` for hostnames and IP addresses. `*` does not match across dots — `192.*` does not match `192.168.1.1`. Use `192.*.*.*` or a CIDR range instead.
:::

### CIDR

Contains `/` and parses as a valid CIDR network. Matches IP addresses within the range.

```kdl
route "10.0.0.0/8" via="internal"
route "fd00::/8" via="internal6"
```

IPv4 and IPv6 are both supported. Non-IP hostnames (e.g. `example.com`) never match CIDR rules.

## Detection Priority

When a pattern could be ambiguous, detection follows this order:

1. Contains `/` and valid CIDR → **CIDR**
2. Contains `*` or `?` → **Glob**
3. Starts with `.` → **Domain suffix**
4. Otherwise → **Exact**

## Matching Behavior

- **Port stripping** — `example.com:8080` is matched as `example.com`
- **Case insensitive** — patterns and hostnames are lowercased before comparison
- **Last match wins** — rules are evaluated in order; the last matching rule determines the upstream
- **No match** — traffic connects directly (no upstream proxy)
- **`via="direct"`** — explicitly routes through no proxy, overriding earlier matches

## Performance

- Patterns are classified and pre-compiled once at config load
- CIDR networks are parsed into `net.IPNet` structs — matching is a single `Contains` call
- Glob patterns use the stdlib `filepath.Match` function
- Domain suffix and exact matches are simple string operations
- No allocations occur during per-request matching (patterns are lowercased at load time)
