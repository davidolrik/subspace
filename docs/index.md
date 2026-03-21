---
layout: home

hero:
  name: Subspace
  text: Transparent proxy with upstream routing
  tagline: Route HTTP, HTTPS, WebSocket, and WSS traffic through upstream proxies based on hostnames and IP ranges — without terminating TLS.
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
  - title: No TLS Termination
    details: Extracts SNI from the TLS ClientHello for routing decisions, then tunnels raw bytes. Your TLS traffic is never decrypted.
  - title: Flexible Routing
    details: Match by exact hostname, domain suffix, glob pattern, or CIDR subnet. Last-match-wins ordering gives you precise control.
  - title: Connection Pooling
    details: Reuses upstream connections across HTTP requests, avoiding repeated TCP and proxy handshakes.
  - title: Hot Reload
    details: Config file changes are detected and applied without restart. Split your config across multiple files with include and glob support.
  - title: Health Monitoring
    details: TCP health checks for all upstreams, per-upstream traffic stats, and live log streaming from a running server.
  - title: Zero-Copy Relay
    details: Tunneled connections (TLS, CONNECT, WebSocket) use kernel-level splice/sendfile for efficient data transfer.
---
