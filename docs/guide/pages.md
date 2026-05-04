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

### Subtitles inside a list

Inside a `list` block you can use a `title "..."` node to break the list into labelled groups. Subtitles render as small uppercase headers above the next group of links and preserve the order they appear in the KDL file:

```kdl
list "Repositories" {
    title "GitHub"
    link "subspace" url="https://github.com/davidolrik/subspace"
    link "kdl-go"   url="https://github.com/sblinch/kdl-go"

    title "GitLab"
    link "internal" url="https://gitlab.example.com/team/internal"
}
```

Subtitles are only used for visual grouping — they aren't included in search results, can't carry tags, and don't accept any properties beyond their name.

### Markdown blocks

Use a `markdown "..."` node to add prose to a page — deprecation notices, ownership info, status callouts, anything that doesn't fit as a link. Markdown is rendered server-side and sanitised so untrusted HTML in the source can't reach the browser.

Markdown can appear in two places:

- **At the top level** of a page, as either a full-width band that breaks the grid (the default — no properties), or as a grid card with an explicit `columns=N` and/or `rows=N` span.
- **Inside a `list` block**, interleaved with links and subtitles, rendered as a muted prose row in the list card. `columns` / `rows` are ignored here.

`columns=N` sets the horizontal span (in grid columns); `rows=N` sets the vertical span. Setting either one puts the markdown in the surrounding grid as a card; setting `rows` without `columns` implies `columns=1` so the card stays a single column wide. With **all** of `columns`, `rows`, and `float` absent the markdown becomes a page-spanning band that splits the page into separate grids before and after.

`columns` is clamped to whatever the grid currently shows (4 cards on desktop, 3 / 2 / 1 at narrower widths) so a `columns=4` card on a phone collapses to one column wide instead of overflowing. `rows` is not clamped — the grid's row count is open-ended.

`float="left"` (default) places the card in the natural left-to-right flow of the grid; `float="right"` pins it to the right edge instead — handy for "owners" or "see also" sidebars. The card width still follows `columns` and clamps the same way at narrow viewports, just anchored to the right.

`color="#hex"` tints a markdown grid card with the same colored top border, glow, and gradient background that `list color="..."` produces — handy when you want a status callout or "owners" card that visually matches one of your section accents. The property is silently ignored on bands (which span the full width and have no card chrome) and on in-list markdown rows (which are inline prose). Omitting `color` keeps the default chrome.

`include="./notes.md"` loads the markdown source from a separate file instead of inline content. Paths are resolved relative to the page's `.kdl` file; absolute paths and `~/`-prefixed paths also work. Included files are watched, so editing them triggers the same hot reload as editing the `.kdl` itself. If both `include=` and an inline value are set, the file is preferred and the inline value is used as a fallback when the file can't be read. If the file is missing and there's no fallback, the dashboard renders a visible "include failed" placeholder card naming the missing path so the problem is impossible to miss.

```kdl
markdown include="./welcome.md"
markdown columns=2 include="~/dashboards/notes.md" "Couldn't load notes file."
```

The grid uses dense packing, so when a multi-row floated card opens a hole on the opposite side, the next lists (and card-width markdowns) flow up into it instead of leaving an empty band. A `markdown columns=2 rows=2 float="right"` next to two short lists on the left will result in two more lists slotting into the 2×2 void below them, not below the floated card.

```kdl
title "Platform"

// No properties → full-width band, breaks the grid.
markdown r#"
## Heads up
The legacy auth proxy is being **decommissioned on 2026-06-01**.
Please migrate to the new SSO gateway before then.

- [Migration guide](https://docs.example.com/sso)
- Slack: `#platform-help`
"#

list "Auth" {
    link "SSO Gateway" url="https://sso.example.com"
    markdown "_The legacy proxy is **deprecated** — see banner above._"
    link "Legacy proxy" url="https://old-auth.example.com"
}

// columns=2 → 2-wide × 1-tall grid card, tinted red.
markdown columns=2 color="#ff375f" r#"
### Heads up
The legacy auth proxy goes away soon — start migrating now.
"#

// rows=2 → 1-wide × 2-tall grid card.
markdown rows=2 r#"
### Quick links
- [Runbook](https://runbook.example.com)
- [Dashboards](https://grafana.example.com)
- [Status](https://status.example.com)
- [Incidents](https://incidents.example.com)
"#

// float=right → 1-wide sidebar pinned to the right edge.
markdown float="right" r#"
### See also
[Status page](https://status.example.com)
"#

list "Observability" {
    link "Grafana" url="https://grafana.example.com"
}
```

CommonMark plus GFM extensions (tables, strikethrough, autolinks, task lists) are supported. **Task list checkboxes are interactive** — click one to toggle it, and the state persists in your browser's localStorage so it survives reloads. State is keyed per (page, label hash), so two pages with identical task wording keep independent state, and renaming a task starts it fresh. Use raw KDL strings (`r#"..."#`) to embed multi-line markdown without having to escape newlines or quotes. All links rendered from markdown open in a new tab.

Subspace strips a common leading indent from every line of a multi-line markdown source so you can keep your config file tidy. The first non-blank line determines the prefix — its leading tabs/spaces are removed from every following line, while lines indented more than the prefix keep their extra whitespace (so nested markdown lists still work). A heredoc-style leading newline is also trimmed.

```kdl
list "Notes" {
    markdown r#"
    ## Indented in source
    But flush-left when rendered.

    - bullet
      - nested bullet (extra indent kept)
    "#
}
```

This means you don't need to fight your editor's auto-indent — write the markdown at whatever indentation matches the surrounding KDL block, and it'll render correctly.

GitHub-flavored alerts also work — start a blockquote with `[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, or `[!CAUTION]` and the dashboard renders it as a coloured callout with a tinted background that stands out against the surrounding card or band. An optional title may follow the marker; otherwise the type's name is used.

| Marker         | Default title | Accent |
|----------------|---------------|--------|
| `[!NOTE]`      | Note          | blue   |
| `[!TIP]`       | Tip           | green  |
| `[!IMPORTANT]` | Important     | purple |
| `[!WARNING]`   | Warning       | amber  |
| `[!CAUTION]`   | Caution       | red    |

```kdl
markdown r#"
> [!WARNING] Read me
> The legacy proxy goes away on 2026-06-01.

> [!TIP]
> You can paste a curl command into the search palette to copy it.
"#
```

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

## Validation and error handling

Subspace tries hard to keep a page reachable even when its KDL has problems — losing the URL while you're editing config is worse than seeing a partial page. The rules:

- **Top-level nodes are validated strictly.** Unknown properties on `title`, `footer`, `list`, or `markdown` (e.g. `markdown wdith="full"` — a typo) are flagged in the config-error banner and via `subspace validate`.
- **Inside a `list` block, validation is lenient.** Unknown properties on `link`, in-list `title`, and in-list `markdown` are silently ignored so an in-progress sketch like `link "x" url="..." note="todo"` doesn't fail the whole page.
- **KDL syntax errors don't drop the page.** If the file fails to parse at the KDL level (mismatched braces, unterminated strings) the page is still registered with empty content, and the error appears in the config-error banner at the top of the dashboard. The URL keeps working — you don't get redirected to "page not defined" mid-edit.
- **Per-node errors don't drop the page either.** A list with one bad link, an unknown child node, or a markdown block whose source fails to render — the offending node is skipped, the rest of the page still renders, and each error is added to the banner.

`subspace validate` exits non-zero whenever the banner would show anything, so you can wire it into CI on a config repo.

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
| `url-encode`  | no       | How the query is encoded before substitution into `{query}`. One of `"component"` (default — `%20` for spaces, `encodeURIComponent`-style), `"form"` (same but spaces become `+`), or `"raw"` (passthrough; the query is inserted verbatim). Use `"form"` for engines whose servers expect form-style encoding, and `"raw"` only when you've pre-encoded the value yourself. |

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

The `{query}` placeholder is replaced with the user's query, encoded according to the engine's `url-encode` mode (default `component` — spaces become `%20`, special characters are percent-encoded). The placeholder may appear multiple times in a single template — all occurrences are replaced with the same encoded value:

```kdl
engine "urlscan" url="https://urlscan.io/search/?q={query}#{query}"
```

If the URL is missing `{query}` entirely, the engine is rejected with a config error.

### Encoding modes

Pick the mode whose output the engine's server expects:

| Mode        | Spaces → | Other special chars | When to use                                                                          |
| ----------- | -------- | ------------------- | ------------------------------------------------------------------------------------- |
| `component` | `%20`    | percent-encoded     | Default. Works for most modern URLs.                                                  |
| `form`      | `+`      | percent-encoded     | Engines that parse the query string as `application/x-www-form-urlencoded` (some older search backends). |
| `raw`       | unchanged | unchanged           | You've already encoded the value yourself, or you're embedding pre-built query strings. |

```kdl
engine "form-style" url="https://example.com/search?q={query}" url-encode="form"
```

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
- **Top activity** — three ranked lists (upstreams, destination hostnames, route patterns) over the same time window as the charts. The metric selector at the top of the section ranks all three lists by total bytes, bytes in, bytes out, successful connections, or failed connections.

All charts support selectable time ranges from 5 minutes to 365 days. Statistics are persisted to a SQLite database at `~/.config/subspace/stats.db` with automatic downsampling (5s resolution to 1m after 1 hour, 1m to 1h after 7 days). Retention defaults to one year; configure it via the [`stats`](/reference/configuration#stats) block — accepts `"30d"`, `"168h"`, `"12h30m"`, etc., or `"forever"` to disable pruning.

The statistics page auto-refreshes every 5 seconds.

## When Subspace Is Not Running

When subspace is not running, requests to `pages.subspace.pub` and `stats.subspace.pub` are handled by an external redirect server that redirects to the documentation site at `https://subspace.pub/`. The redirect server also handles HTTPS → HTTP redirection so the daemon can intercept plain HTTP requests when it is running.

## Error Pages

When a connection through the proxy fails — due to DNS resolution errors, upstream dial failures, or other connection problems — Subspace shows a styled error page with the hostname, error details, and the upstream that was used. These replace the bare HTTP 502 responses that a typical proxy would return.
