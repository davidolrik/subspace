# Quick Start

## Create a Config File

The default config path is `~/.config/subspace/config.kdl`. Subspace uses [KDL](https://kdl.dev) as its configuration language.

```sh
mkdir -p ~/.config/subspace
```

Create `~/.config/subspace/config.kdl`:

```kdl
listen ":8118"

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

This config:
- Listens on port 8118
- Defines two upstreams: an HTTP CONNECT proxy and a SOCKS5 proxy
- Routes `.corp.internal` subdomains through the corporate proxy
- Routes `specific.host.com` through the SOCKS5 tunnel
- Everything else connects directly

## Start the Proxy

```sh
subspace serve
```

You should see:

```
INF subspace listening version=dev addr=:8118 upstreams=2 routes=2
INF watching config for changes files=1
```

## Test It

```sh
# HTTP — routed based on Host header
curl -x http://localhost:8118 http://app.corp.internal/api

# HTTPS — routed based on CONNECT target
curl -x http://localhost:8118 https://app.corp.internal/api

# SOCKS5 — same port, auto-detected
curl --socks5-hostname localhost:8118 https://app.corp.internal/api

# Direct — no matching route, connects directly
curl -x http://localhost:8118 https://example.com
```

SOCKS5 clients like `git` and `ssh` can use the same port:

```sh
git -c http.proxy=socks5h://localhost:8118 clone https://github.com/org/repo
```

## Check Status

In another terminal:

```sh
subspace status
```

This shows the health of each upstream (TCP reachability), traffic stats, and connection pool state.

## Check Routing

Test which upstream would handle a given URL without sending traffic:

```sh
subspace resolve https://app.corp.internal/api
```

## Next Steps

- [Configuration](/guide/configuration) — full config reference
- [Routing](/guide/routing) — pattern matching in detail
- [Commands](/guide/commands) — all CLI commands
