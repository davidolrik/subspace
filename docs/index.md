---
layout: home

hero:
  name: Subspace
  text: Transparent proxy with upstream routing
  tagline: Route HTTP, HTTPS, WebSocket, SOCKS5, and WSS traffic through upstream proxies based on hostnames and IP ranges — without terminating TLS.
  image:
    src: /subspace.png
    alt: Subspace
  actions:
    - theme: brand
      text: Get Started
      link: /guide/what-is-subspace
    - theme: alt
      text: Configuration
      link: /reference/configuration

features:
  - icon: "\U0001F510"
    title: No TLS Termination
    details: Extracts SNI from the TLS ClientHello for routing decisions, then tunnels raw bytes. Your TLS traffic is never decrypted.
  - icon: "\U0001F9ED"
    title: Flexible Routing
    details: Match by exact hostname, domain suffix, glob pattern, or CIDR subnet. Last-match-wins ordering gives you precise control.
  - icon: "\U0001F9E6"
    title: SOCKS5 Inbound
    details: Accepts SOCKS5 clients on the same port as HTTP, auto-detected from the first byte. Works with git, ssh, curl, and any SOCKS5-aware tool.
  - icon: "\U0001F4D1"
    title: Internal Pages
    details: Built-in link dashboards and live statistics served at *.subspace hostnames. Press / to search across all pages, links, and descriptions.
  - icon: "\U0001F504"
    title: Hot Reload
    details: Config file changes are detected and applied without restart. Split your config across multiple files with include and glob support.
  - icon: "\U0001F4CA"
    title: Statistics
    details: Live metrics, upstream health, and historical charts with persistent SQLite storage. Data retained for one year with automatic downsampling.
  - icon: "\U0000267B"
    title: Connection Pooling
    details: Reuses upstream connections across HTTP requests, avoiding repeated TCP and proxy handshakes.
  - icon: "\U0001F3E5"
    title: Health Monitoring
    details: TCP health checks for all upstreams, per-upstream traffic stats, and live log streaming from a running server.
  - icon: "\U000026A1"
    title: Zero-Copy Relay
    details: Tunneled connections (TLS, CONNECT, WebSocket) use kernel-level splice/sendfile for efficient data transfer.
---
