# Internal Pages

Subspace serves internal pages at `pages.subspace.pub` (alias `p.subspace.pub`) when browsing through the proxy. These pages provide link dashboards for organizing bookmarks and a live statistics view — all without leaving the browser.

## Overview

All link pages are served under `pages.subspace.pub/{name}/`, with each page getting its own path. Statistics is available at `stats.subspace.pub`. Pages are defined in KDL files and configured via `page` directives in the main config.

```kdl
page "dev.kdl"
page "ops.kdl" alias="o"
```

This creates:

- `http://pages.subspace.pub/dev/` — links from `dev.kdl`
- `http://pages.subspace.pub/ops/` (or `http://p.subspace.pub/o/`) — links from `ops.kdl`
- `http://stats.subspace.pub/` — built-in statistics (always available)
- `http://pages.subspace.pub/` — redirects to the first configured page

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

| Property      | Required | Description                                                                                                                                                                                                                                                |
| ------------- | -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `url`         | Yes      | The link URL                                                                                                                                                                                                                                               |
| `icon`        | No       | Icon name — `si-*` for [Simple Icons](https://simpleicons.org), `fa-*` for [Font Awesome](https://fontawesome.com/icons), `mdi-*` for [Material Design Icons](https://pictogrammers.com/library/mdi/), `nf-*` for [Nerd Fonts](https://www.nerdfonts.com/) |
| `description` | No       | Short description shown below the link name                                                                                                                                                                                                                |

### Section colors and icons

Sections can have an accent color that tints the card border and background, and an icon displayed in the top-right corner of the card:

```kdl
list "Critical" color="#ff375f" icon="fa-fire" {
    link "Incidents" url="https://incidents.example.com" icon="fa-triangle-exclamation"
}
```

The section icon uses the same color as the section, with a subtle glow. If no color is set, the icon uses a muted default color. Icons use the same `si-*`, `fa-*`, `mdi-*`, and `nf-*` naming as link icons.

### Tags

Tags are small colored pills used to label links and entire sections — for example to mark something as `prod`, `internal`, or `wip`. They are defined once in the main config so the same color palette applies to every page:

```kdl
// in subspace.kdl
tags {
    tag "prod"     color="#00ff88"
    tag "internal" color="#ff6b6b"
    tag "wip"      color="#ffaa00"
}
```

Reference them from links and lists in any page KDL file using the `tags` property. Multiple tags are space-separated:

```kdl
list "Dev" tags="internal" {
    link "GitHub"        url="https://github.com" tags="prod external"
    link "Internal Wiki" url="https://wiki"       tags="internal wip"
}
```

- Tags on a `link` render as inline pills, right-aligned after the link.
- Tags on a `list` render as a row of pills along the bottom of the section card, left-aligned.

Referencing a tag that is not defined in the global `tags` block causes a validation error at startup (and a warning on hot reload, with the previous configuration left in place).

#### Aliases

A tag's reference name must be unique, but the text shown on its pill can be overridden with `alias`. Aliases may repeat across tags, so you can render the same display label in different colors:

```kdl
tags {
    tag "services"         color="#00ff88"
    tag "olrikit_services" color="#ff0088" alias="services"
}
```

Both pills above show the text "services" but use different colors, letting you distinguish (for example) generic services from a specific provider's services at a glance.

## Page Names and Aliases

By default, the page name is derived from the filename (minus the `.kdl` extension). Override it with `name=`, and add an alias with `alias=`:

| Config                            | URL                                                             |
| --------------------------------- | --------------------------------------------------------------- |
| `page "dev.kdl"`                  | `http://pages.subspace.pub/dev/`                                |
| `page "my-file.kdl" name="tools"` | `http://pages.subspace.pub/tools/`                              |
| `page "ops.kdl" alias="o"`        | `http://pages.subspace.pub/ops/` and `http://p.subspace.pub/o/` |

If a page is named `stats` or `statistics`, it will be accessible at `pages.subspace.pub/stats/` alongside the statistics page at `stats.subspace.pub`.

## Navigation

All configured pages and the statistics page appear in a shared navigation menu at the top of every page. Pages are shown in the order they are defined in the config. The menu also includes icon links to the documentation and GitHub repository.

The currently active page is highlighted in the menu.

## Search

Press `/` on any internal page to open the search popup. Search works across all pages, not just the one you're currently viewing, and can route queries through external search engines you configure.

### What is searched

The search matches against:

- **Page titles** — the `title` from each page's KDL file
- **Page names** — the primary name and alias
- **Link names** — the name of every link across all pages
- **Link descriptions** — the `description` property of links
- **Engine keywords** — names and aliases of any [search engines](#search-engines) you've configured

### Result ordering

Results appear in this order:

1. **Engine row** — when the first token of your query matches an engine name or alias exactly (e.g. `cpan ojo`), a row at the top routes the rest of the query through that engine.
2. **Engine prefix rows** — while you're still typing the first token, every engine whose name or alias starts with what you've typed appears as a candidate, so Tab can autocomplete the keyword.
3. **Pages** — matching pages, statistics, documentation, and GitHub links.
4. **Links** — matching links from any page, shown with their page and section as context.
5. **Fallback engines** — when nothing else matched, a row is rendered for the configured [default engine](#default-engine) plus any engine declared with `fallback=#true`. Engines without `fallback` (and not designated as the default) stay keyword-only and never appear in this list.

Within each group, prefix matches rank higher than substring matches. For example, typing `s` shows "Statistics" before "Dashboard" (which contains an `s` but not at the start). Engine name/alias matching is case-insensitive — `MetaCPAN`, `metacpan`, and `MetaCpan` all resolve to the same engine, while the original casing is preserved on the engine row label.

### Tab autocomplete

Press `Tab` to extend your input to the longest unambiguous prefix of the visible candidates — like shell tab completion:

- One candidate → completes to the full label (or full engine name with a trailing space, ready for the query).
- Multiple candidates with a shared prefix → input extends to that shared prefix and you keep typing.
- Nothing more to extend (current input is already the longest common prefix) → the modal border flashes purple to signal "type more".

Only labels that themselves extend what you typed are considered. Rows that surfaced via secondary fields (e.g. a link returned because its page name matched) don't block completion.

### Keyboard shortcuts

| Key                       | Action                                                                            |
| ------------------------- | --------------------------------------------------------------------------------- |
| `/`                       | Open search                                                                       |
| `Escape`                  | Close search                                                                      |
| `Arrow Up` / `Arrow Down` | Navigate results                                                                  |
| `Tab`                     | Autocomplete to the longest unambiguous shared prefix                             |
| `Enter`                   | Go to selected result (or expand keyword on a prefix row)                         |
| `Cmd`+`Enter` / `Ctrl`+`Enter` | Open selected result in a new tab; the search modal stays open for the next query |

You can also click any result or click outside the popup to close it. `Cmd`/`Ctrl`-click on a result also opens it in a new tab via the browser's native link handling.

## Search Engines

External search engines let you route queries from the `/` palette to sites like Google, Metacpan, GitHub, urlscan, etc. — without leaving the palette. Engines are declared in your main config alongside `tags { ... }`:

```kdl
search-engines default="google" {
    engine "google"   url="https://www.google.com/search?q={query}"        icon="si-google"     alias="g"
    engine "metacpan" url="https://metacpan.org/search?q={query}"          icon="fa-cube"       alias="cpan"
    engine "github"   url="https://github.com/search?q={query}&type=code"  icon="si-github"     alias="gh"
    engine "urlscan"  url="https://urlscan.io/search/?q={query}"           icon="si-urlscan"
    engine "ddg"      url="https://duckduckgo.com/?q={query}"              icon="si-duckduckgo"
}
```

### Engine fields

| Field         | Required | Description                                                                                                                                              |
| ------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| name          | yes      | Positional argument. The primary keyword used to invoke the engine. Must be unique (case-insensitive — `Google` and `google` collide).                  |
| `url`         | yes      | Engine URL template. **Must contain the literal `{query}` placeholder.** Every occurrence is replaced with the URL-encoded query at navigation time.    |
| `alias`       | no       | Additional keyword that triggers the same engine. Useful for short forms like `g` for `google` or `cpan` for `metacpan`.                                |
| `icon`        | no       | Same icon system as links: `si-*`, `fa-*`, `mdi-*`, `nf-*`. When omitted, subspace fetches the engine host's `/favicon.ico` once, caches it server-side, and serves it from `/api/favicon` with a 24-hour browser cache; missing favicons fall back to a magnifier glyph. |
| `description` | no       | Short text shown as the third line of the engine's result row, mirroring how link descriptions render on link rows.                                      |
| `fallback`    | no       | When `#true`, the engine appears in the no-match fallback list alongside the default engine. Defaults to `#false` so niche engines stay keyword-only.    |

### Default engine

The block-level `default=` property names the engine shown first in the no-match fallback list. When your query matches no page, link, or engine keyword, the dashboard renders one row per fallback-eligible engine — the default first (when set), followed by each engine with `fallback=#true`, alphabetised by name. Without a `default=` and no `fallback=#true` engines, queries with no matches simply produce empty results.

The default reference is case-insensitive and must point at an engine declared in the same block — an unknown reference is downgraded to a non-fatal config error and the field is cleared (you'll see it in the config error banner). The default engine is implicitly part of the fallback list, so you don't need to set `fallback=#true` on it.

```kdl
search-engines default="google" {
    engine "google"  url="https://www.google.com/search?q={query}"
    engine "kagi"    url="https://kagi.com/search?q={query}"     fallback=#true
    engine "ddg"     url="https://duckduckgo.com/?q={query}"     fallback=#true
    engine "urlscan" url="https://urlscan.io/search/?q={query}"  // keyword-only
}
```

With this config, an unknown query like `xyzzy` shows three fallback rows — google (default, first), then ddg and kagi alphabetically. urlscan only fires when you type its keyword.

### URL placeholder

The `{query}` placeholder is replaced with `encodeURIComponent(query)`, so spaces and special characters are URL-safe. The placeholder may appear multiple times in a single template — all occurrences are replaced with the same encoded value:

```kdl
engine "urlscan" url="https://urlscan.io/search/?q={query}#{query}"
```

If the URL is missing `{query}` entirely, the engine is rejected with a config error.

### Hot reload

Search engines hot-reload like the rest of the config — edit your KDL, save, and every open dashboard tab automatically reloads within a few seconds (the dashboard polls a config-version counter on the `/api/config-errors` endpoint and refreshes when it changes).

### Examples

Type `cpan ojo` → top row "Search metacpan for "ojo"", press `Enter` → opens `https://metacpan.org/search?q=ojo`.

Type `cp` → engine-prefix row for `metacpan` appears, press `Tab` → input becomes `metacpan⎵` and you can keep typing the query.

Type `xyzzy-no-such-thing` (with `default="google"`) → fallback row "Search google for "xyzzy-no-such-thing"", press `Enter` → opens Google.

## Statistics Page

The statistics page is always available at `http://stats.subspace.pub/` (or `http://statistics.subspace.pub/`). It shows:

- **Live metrics** — total connections, active connections, and upstream count
- **Upstream health** — health status, type, address, latency, and traffic stats for each upstream
- **Connections over time** — line chart showing new connections, active connections, and errors
- **Traffic by upstream** — stacked bar chart of bytes transferred per upstream
- **Protocol breakdown** — pie chart of connections by protocol (HTTP, TLS, SOCKS5, CONNECT, WebSocket)

All charts support selectable time ranges from 5 minutes to 365 days. Statistics are persisted to a SQLite database at `~/.config/subspace/stats.db` and retained for one year with automatic downsampling (5s resolution to 1m after 1 hour, 1m to 1h after 7 days).

The statistics page auto-refreshes every 5 seconds.

## When Subspace Is Not Running

When subspace is not running, requests to `pages.subspace.pub` and `stats.subspace.pub` are handled by an external redirect server that redirects to the documentation site at `https://subspace.pub/`. The redirect server also handles HTTPS → HTTP redirection so the daemon can intercept plain HTTP requests when it is running.

## Error Pages

When a connection through the proxy fails — due to DNS resolution errors, upstream dial failures, or other connection problems — Subspace shows a styled error page with the hostname, error details, and the upstream that was used. These replace the bare HTTP 502 responses that a typical proxy would return.
