# Internal Pages

Subspace serves internal pages at `*.subspace` hostnames when browsing through the proxy. These pages provide link dashboards for organizing bookmarks and a live statistics view — all without leaving the browser.

## Overview

Every page gets its own `*.subspace` hostname and appears in a shared navigation menu at the top. Pages are defined in KDL files and configured via `page` directives in the main config.

```kdl
page "dev.kdl"
page "ops.kdl" alias="o"
```

This creates:

- `http://dev.subspace/` — links from `dev.kdl`
- `http://ops.subspace/` (or `http://o.subspace/`) — links from `ops.kdl`
- `http://stats.subspace/` — built-in statistics (always available)

All pages share a navigation menu, search, and dark theme. Icons and fonts are embedded in the binary — no external requests are made.

## Link Pages

A link page is a KDL file with an optional title, optional footer, and one or more named sections containing links.

```kdl
title "Development Tools"
footer "Acme Corp — Internal Use Only"

list "Repositories" {
    link "GitHub" url="https://github.com/org" icon="si-github" description="Source code"
    link "GitLab" url="https://gitlab.corp.com" icon="si-gitlab"
}

list "Monitoring" {
    link "Grafana" url="https://grafana.example.com" icon="si-grafana" description="Dashboards"
    link "PagerDuty" url="https://pagerduty.com" icon="fa-bell"
}
```

Links are displayed in a responsive grid of cards, one card per section. Each link shows its icon, name, and optional description.

### Link properties

| Property | Required | Description |
|---|---|---|
| `url` | Yes | The link URL |
| `icon` | No | Icon name — `si-*` for [Simple Icons](https://simpleicons.org), `fa-*` for [Font Awesome](https://fontawesome.com/icons) |
| `description` | No | Short description shown below the link name |

### Section colors

Sections can have an accent color that tints the card border and background:

```kdl
list "Critical" color="#ff375f" {
    link "Incidents" url="https://incidents.example.com" icon="fa-fire"
}
```

## Hostnames and Aliases

By default, the hostname is derived from the filename (minus the `.kdl` extension). Override it with `host=`, and add a second hostname with `alias=`:

| Config | URL |
|---|---|
| `page "dev.kdl"` | `http://dev.subspace/` |
| `page "my-file.kdl" host="tools"` | `http://tools.subspace/` |
| `page "ops.kdl" alias="o"` | `http://ops.subspace/` and `http://o.subspace/` |

The hostnames `stats` and `statistics` are reserved for the built-in statistics page and cannot be used.

## Navigation

All configured pages and the statistics page appear in a shared navigation menu at the top of every page. Pages are shown in the order they are defined in the config. The menu also includes icon links to the documentation and GitHub repository.

The currently active page is highlighted in the menu.

## Search

Press `/` on any internal page to open the search popup. Search works across all pages, not just the one you're currently viewing.

### What is searched

The search matches against:

- **Page titles** — the `title` from each page's KDL file
- **Page hostnames** — the primary hostname and alias
- **Link names** — the name of every link across all pages
- **Link descriptions** — the `description` property of links

### Result ordering

Results are split into two groups, with pages always appearing before links:

1. **Pages** — matching pages, statistics, documentation, and GitHub links
2. **Links** — matching links from any page, shown with their page and section as context

Within each group, prefix matches rank higher than substring matches. For example, typing `s` shows "Statistics" before "Dashboard" (which contains an `s` but not at the start).

### Keyboard shortcuts

| Key | Action |
|---|---|
| `/` | Open search |
| `Escape` | Close search |
| `Arrow Up` / `Arrow Down` | Navigate results |
| `Enter` | Go to selected result |

You can also click any result or click outside the popup to close it.

## Statistics Page

The statistics page is always available at `http://stats.subspace/` (or `http://statistics.subspace/`). It shows:

- **Live metrics** — total connections, active connections, and upstream count
- **Upstream health** — health status, type, address, latency, and traffic stats for each upstream
- **Connections over time** — line chart showing new connections, active connections, and errors
- **Traffic by upstream** — stacked bar chart of bytes transferred per upstream
- **Protocol breakdown** — pie chart of connections by protocol (HTTP, TLS, SOCKS5, CONNECT, WebSocket)

All charts support selectable time ranges from 5 minutes to 365 days. Statistics are persisted to a SQLite database at `~/.config/subspace/stats.db` and retained for one year with automatic downsampling (5s resolution to 1m after 1 hour, 1m to 1h after 7 days).

The statistics page auto-refreshes every 5 seconds.

## Entry Point

Navigating to `http://subspace.dk/` redirects to the first configured page, or to the statistics page if no pages are defined.

When subspace is not running, `subspace.dk` resolves to a real web server that redirects to the [troubleshooting guide](/guide/troubleshooting#not-running).

## Error Pages

When a connection through the proxy fails — due to DNS resolution errors, upstream dial failures, or other connection problems — Subspace shows a styled error page with the hostname, error details, and the upstream that was used. These replace the bare HTTP 502 responses that a typical proxy would return.
