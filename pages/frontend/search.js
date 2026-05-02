// Search popup component for Subspace internal pages.
// Activated by pressing "/" — searches page titles, hostnames, aliases,
// and links, and routes external queries through configured search
// engines (e.g. "cpan ojo" → Metacpan).

// Score a single token against the candidate fields: lower is better.
// Prefix matches on the primary field (label/name) rank highest, then
// prefix on secondary fields, then substring matches.
export function tokenScore(fields, token) {
    let best = Infinity;
    for (let i = 0; i < fields.length; i++) {
        const f = fields[i];
        if (!f) continue;
        const lower = f.toLowerCase();
        const pos = lower.indexOf(token);
        if (pos === -1) continue;
        // prefix on primary field (i=0) = 0, prefix on secondary = 1,
        // substring on primary = 2, substring on secondary = 3
        const score = (pos === 0 ? 0 : 2) + (i === 0 ? 0 : 1);
        if (score < best) best = score;
    }
    return best;
}

// Score a (possibly multi-word) query against fields. Every word must
// match somewhere in the fields; the total score is the sum of
// per-word scores. A contiguous phrase match on the full query is
// preferred and gets a bonus (lower score).
export function matchScore(fields, q) {
    const phraseScore = tokenScore(fields, q);
    const words = q.split(/\s+/).filter(w => w.length > 0);
    if (words.length <= 1) return phraseScore;
    let total = 0;
    for (const w of words) {
        const s = tokenScore(fields, w);
        if (s === Infinity) return Infinity;
        total += s;
    }
    if (phraseScore !== Infinity && phraseScore < total) return phraseScore;
    return total;
}

// matchEngine looks up an engine whose name or alias equals the given
// token (case-insensitive). Returns the engine record or undefined.
export function matchEngine(engines, token) {
    if (!token) return undefined;
    const t = token.toLowerCase();
    return engines.find(e =>
        e.name.toLowerCase() === t || (e.alias && e.alias.toLowerCase() === t)
    );
}

// matchEnginePrefixes returns every engine whose name or alias starts
// with the given token (case-insensitive). Used to surface partial
// keyword matches in the result list so Tab can autocomplete them.
// Engines whose name or alias matches exactly are dropped — the exact
// match has already been emitted as the primary engine row.
export function matchEnginePrefixes(engines, token) {
    if (!token) return [];
    const t = token.toLowerCase();
    const out = [];
    for (const e of engines) {
        const name = e.name.toLowerCase();
        const alias = e.alias ? e.alias.toLowerCase() : '';
        if (name === t || alias === t) continue;
        if (name.startsWith(t) || (alias && alias.startsWith(t))) {
            out.push(e);
        }
    }
    return out;
}

// encodeQuery applies the engine's chosen url-encode mode to the
// user's query. Modes:
//   - "" / "component" — encodeURIComponent; spaces become %20.
//   - "form"           — same, but spaces become "+".
//   - "raw"            — no transformation; the query is inserted
//                        verbatim. Use this for engines whose URL
//                        grammar already embeds an encoded value.
export function encodeQuery(query, mode) {
    switch (mode) {
        case 'raw':
            return query;
        case 'form':
            return encodeURIComponent(query).replace(/%20/g, '+');
        case '':
        case undefined:
        case null:
        case 'component':
            return encodeURIComponent(query);
        default:
            return encodeURIComponent(query);
    }
}

// buildEngineURL substitutes the user's query into an engine's URL
// template by replacing every "{query}" with the encoded query. The
// engine's `urlEncode` field selects the encoding strategy.
export function buildEngineURL(engine, query) {
    const encoded = encodeQuery(query, engine && engine.urlEncode);
    return engine.url.replace(/\{query\}/g, encoded);
}

// engineFaviconURL returns the dashboard's cached-favicon endpoint
// URL for an engine. The backend fetches /favicon.ico from the engine
// host once per host, caches the bytes, and serves them with long
// Cache-Control headers — so result rows for engines without an
// explicit icon show recognisable branding without re-fetching from
// the engine origin on every page load. Returns null for engines
// missing or with unparseable URLs.
export function engineFaviconURL(engine) {
    if (!engine || !engine.url) return null;
    try {
        const u = new URL(engine.url);
        if (!u.host) return null;
        return 'api/favicon?host=' + encodeURIComponent(u.host);
    } catch (e) {
        return null;
    }
}

// navigationIntent returns the URL the modal should navigate to and
// whether it should open in a new tab, given a result row and the
// modifier keys held when the user pressed Enter (or clicked).
// Returns null when the row has no destination — engine-prefix rows
// expand the keyword instead of navigating, and an absent result is a
// no-op.
export function navigationIntent(result, modifiers) {
    if (!result || result.type === 'engine-prefix') return null;
    const url = result.type === 'engine'
        ? buildEngineURL(result.engine, result.query)
        : result.url;
    const newTab = !!(modifiers && (modifiers.metaKey || modifiers.ctrlKey));
    return { url, newTab };
}

function navMeta(item) {
    if (!item.name) return item.url;
    return item.url.replace(/^https?:\/\//, '').replace(/\/$/, '');
}

// buildResults assembles the rows shown in the search palette from the
// current query and available data. Pure function so it can be tested
// without booting Alpine. Returns rows in display order:
//
//   1. Optional engine row when the first token is a recognised
//      keyword. The remainder of the query becomes the engine query
//      and tail tokens still run normal page/link search below.
//   2. Page (nav) matches for the query.
//   3. Link matches for the query.
//   4. Optional default-engine fallback row when nothing else matched.
export function buildResults({ query, nav, allLinks, engines, defaultEngine }) {
    const raw = query || '';
    const trimmed = raw.trim();
    const q = trimmed.toLowerCase();

    // Empty query: show all nav/page rows as today (the dashboard's
    // "menubar links" doubling as a quick-launch list).
    if (!q) {
        return nav.map(item => ({
            type: 'page',
            label: item.label,
            url: item.url,
            icon: item.icon,
            meta: navMeta(item),
        }));
    }

    const out = [];
    let engineRowQuery = q; // query used for normal scoring; trimmed of keyword if matched
    let keywordMatched = null;

    // Find the first whitespace in the *un-trimmed* input so a trailing
    // space (e.g. "cp ") signals the user has committed to the token —
    // even though trimmed has no space, we should not surface partial
    // prefix suggestions any more.
    const rawFirstSpace = raw.search(/\s/);
    const stillTypingFirstToken = rawFirstSpace === -1;

    const firstSpace = trimmed.indexOf(' ');
    const firstToken = (firstSpace === -1 ? trimmed : trimmed.slice(0, firstSpace)).trim();
    const tail = firstSpace === -1 ? '' : trimmed.slice(firstSpace + 1).trim();
    const keyword = matchEngine(engines || [], firstToken);
    if (keyword) {
        keywordMatched = keyword;
        engineRowQuery = tail.toLowerCase();
        out.push({
            type: 'engine',
            engine: keyword,
            query: tail,
            label: 'Search ' + keyword.name + ' for "' + tail + '"',
            icon: keyword.icon,
            meta: '',
            description: keyword.description,
        });
    } else if (stillTypingFirstToken) {
        // The user is still typing the first token — surface every
        // engine whose name or alias starts with that prefix so Tab
        // can autocomplete the keyword.
        const prefixes = matchEnginePrefixes(engines || [], firstToken);
        for (const e of prefixes) {
            out.push({
                type: 'engine-prefix',
                engine: e,
                query: '',
                label: e.name + (e.alias ? ' (' + e.alias + ')' : ''),
                icon: e.icon,
                meta: 'Search engine',
                description: e.description,
            });
        }
    }

    // Pages (nav) — score against the query that *follows* a matched
    // keyword, so "cpan ojo" still surfaces a local page named "ojo"
    // below the engine row. When no keyword matched, this is the full
    // query.
    const pages = [];
    const scoreQuery = keywordMatched ? engineRowQuery : q;
    if (scoreQuery) {
        for (const item of nav) {
            const score = matchScore([item.label, item.name, item.alias], scoreQuery);
            if (score < Infinity) {
                pages.push({
                    type: 'page',
                    label: item.label,
                    url: item.url,
                    icon: item.icon,
                    meta: navMeta(item),
                    score,
                });
            }
        }
        pages.sort((a, b) => a.score - b.score);
    }

    const links = [];
    if (scoreQuery) {
        for (const link of allLinks) {
            const score = matchScore(
                [link.name, link.section, link.page, link.description], scoreQuery
            );
            if (score < Infinity) {
                links.push({
                    type: 'link',
                    label: link.name,
                    url: link.url,
                    icon: link.icon,
                    description: link.description,
                    meta: link.page + ' / ' + link.section,
                    score,
                });
            }
        }
        links.sort((a, b) => a.score - b.score);
    }

    out.push(...pages, ...links);

    // Fallback list: when nothing surfaced — no exact keyword, no
    // engine prefix suggestions, no local matches — render one row
    // per fallback-eligible engine so the user always has a
    // destination. The configured default engine is always included
    // (and shown first); additional engines opt in via fallback=#true.
    // Engines that don't opt in stay keyword-only.
    const hasPrefixRows = out.some(r => r.type === 'engine-prefix');
    if (!keywordMatched && !hasPrefixRows && pages.length === 0 && links.length === 0) {
        const fallbackList = collectFallbackEngines(engines || [], defaultEngine);
        for (const e of fallbackList) {
            out.push({
                type: 'engine',
                engine: e,
                query: trimmed,
                label: 'Search ' + e.name + ' for "' + trimmed + '"',
                icon: e.icon,
                meta: '',
                description: e.description,
                // fallback marker so tabCompleteFrom doesn't treat
                // these as exact-keyword rows (the user did not type
                // the engine's name or alias).
                fallback: true,
            });
        }
    }

    return out;
}

// collectFallbackEngines returns the engines shown in the no-match
// fallback list, in display order: the configured default first (when
// it resolves), followed by engines opted in via `fallback=#true`,
// alphabetised by name. The default is omitted from the alphabetised
// remainder so it never appears twice.
export function collectFallbackEngines(engines, defaultEngine) {
    const list = [];
    const seen = new Set();
    let defEngine = null;
    if (defaultEngine) {
        const target = defaultEngine.toLowerCase();
        defEngine = engines.find(e => e.name.toLowerCase() === target) || null;
    }
    if (defEngine) {
        list.push(defEngine);
        seen.add(defEngine.name.toLowerCase());
    }
    const optIns = engines
        .filter(e => e.fallback && !seen.has(e.name.toLowerCase()))
        .slice()
        .sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
    list.push(...optIns);
    return list;
}

// autocompleteFor returns the new input string when the user clicks
// (or presses Enter on) a single, unambiguous row. For engine rows we
// replace just the keyword token (preserving any trailing query the
// user already typed); for engine-prefix rows we expand the partial
// keyword to the engine's full name; for page/link rows we drop the
// whole input and write the row's label.
export function autocompleteFor(result, currentQuery) {
    if (!result) return currentQuery;
    if (result.type === 'engine' || result.type === 'engine-prefix') {
        const parts = currentQuery.split(/\s+/);
        const tail = parts.slice(1).join(' ');
        return result.engine.name + (tail ? ' ' + tail : ' ');
    }
    return result.label;
}

// commonPrefix returns the longest case-insensitive shared prefix of
// the given strings, using the case from the first string as the
// canonical form. Returns "" when there is no shared prefix.
export function commonPrefix(strings) {
    if (!strings || strings.length === 0) return '';
    if (strings.length === 1) return strings[0];
    let prefix = strings[0];
    for (let i = 1; i < strings.length; i++) {
        const s = strings[i];
        let j = 0;
        const max = Math.min(prefix.length, s.length);
        while (j < max && prefix[j].toLowerCase() === s[j].toLowerCase()) j++;
        prefix = prefix.slice(0, j);
        if (prefix === '') break;
    }
    return prefix;
}

// tabCompleteFrom is the Tab handler's smart completion. Behaviour:
//
//   - If the top result is an exact engine-keyword match (`engine`
//     row), expand the keyword preserving any tail query the user
//     already typed (e.g. "cpan ojo" → "metacpan ojo").
//   - Otherwise compute the longest common prefix of every visible
//     candidate (engine-prefix engine name, page/link label). If the
//     prefix is strictly longer than what the user has typed, return
//     it; if a single engine-prefix row matched, append a trailing
//     space so the user can immediately keep typing the query.
//   - Returns null when there is nothing useful to expand to — the
//     caller should flash the modal border to signal "type more".
export function tabCompleteFrom(results, currentQuery) {
    if (!results || results.length === 0) return null;

    // Exact-keyword expansion takes precedence so "cpan ojo" Tab still
    // routes to metacpan even when local matches are also present.
    // Fallback rows are also `type: 'engine'` but the user did not
    // type the engine's keyword, so they should not be treated as
    // keyword-expansion candidates.
    const top = results[0];
    if (top && top.type === 'engine' && !top.fallback) {
        const parts = currentQuery.split(/\s+/);
        const tail = parts.slice(1).join(' ');
        return top.engine.name + (tail ? ' ' + tail : ' ');
    }

    // Page/link rows can show up because the query matched a *secondary*
    // field (e.g. a link's page or section name). Those rows shouldn't
    // count as completion candidates — only labels that themselves
    // extend the user's input do. Engine-prefix rows are always
    // candidates: matchEnginePrefixes already filtered them by name or
    // alias prefix, and the user might be typing toward the alias.
    const queryLower = currentQuery.trim().toLowerCase();
    const candidates = [];
    let allEnginePrefix = true;
    for (const r of results) {
        if (r.type === 'engine-prefix') {
            candidates.push(r.engine.name);
        } else if (r.type === 'page' || r.type === 'link') {
            if (queryLower && r.label.toLowerCase().startsWith(queryLower)) {
                candidates.push(r.label);
                allEnginePrefix = false;
            }
        }
    }
    if (candidates.length === 0) return null;

    const lcp = commonPrefix(candidates);
    if (lcp.length <= currentQuery.length) return null;

    if (candidates.length === 1 && allEnginePrefix) {
        return lcp + ' ';
    }
    return lcp;
}

function iconClass(icon, type) {
    if ((type === 'engine' || type === 'engine-prefix') && !icon) return 'fa-solid fa-magnifying-glass';
    if (!icon) return 'fa-solid fa-link';
    if (icon.startsWith('si-')) return 'si ' + icon;
    if (icon.startsWith('fa-')) return 'fa-solid ' + icon;
    if (icon.startsWith('mdi-')) return 'mdi ' + icon;
    if (icon.startsWith('nf-')) return 'nf ' + icon;
    return icon;
}

// Alpine wiring. Guarded so this module stays importable in plain Node
// (e.g. Vitest) where `document` does not exist.
if (typeof document !== 'undefined' && typeof document.addEventListener === 'function') {
    document.addEventListener('alpine:init', () => {
        Alpine.data('search', () => ({
            open: false,
            query: '',
            selectedIndex: 0,
            allLinks: [],
            engines: [],
            defaultEngine: '',
            // flashing toggles a CSS class on the modal whenever Tab
            // can't extend the input further (ambiguous prefix).
            flashing: false,
            _flashTimer: null,
            // faviconFailed remembers engines whose /favicon.ico load
            // failed so the next render falls back to the magnifier
            // icon instead of showing a broken-image silhouette.
            faviconFailed: {},
            // helpOpen toggles the keyboard-shortcut legend modal.
            helpOpen: false,
            // Expose the keybinding registry to the template so it
            // can render the legend without duplicating the data.
            keybindings,
            keybindingGroups() { return groupBindingsForLegend(keybindings); },
            // nav is injected from the parent component via x-data merge
            nav: [],

            filteredResults() {
                return buildResults({
                    query: this.query,
                    nav: this.nav,
                    allLinks: this.allLinks,
                    engines: this.engines,
                    defaultEngine: this.defaultEngine,
                });
            },

            show() {
                this.open = true;
                this.query = '';
                this.selectedIndex = 0;
                this.$nextTick(() => {
                    this.$refs.searchInput?.focus();
                });
            },

            hide() {
                this.open = false;
            },

            onKeydown(e) {
                if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    const len = this.filteredResults().length;
                    this.selectedIndex = len > 0 ? (this.selectedIndex + 1) % len : 0;
                    this.scrollToSelected();
                } else if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    const len = this.filteredResults().length;
                    this.selectedIndex = len > 0 ? (this.selectedIndex - 1 + len) % len : 0;
                    this.scrollToSelected();
                } else if (e.key === 'Tab') {
                    // Override the browser's native focus-traversal of
                    // the result <a> tags: Tab now does shell-style
                    // longest-common-prefix completion across the
                    // visible results.
                    e.preventDefault();
                    const next = tabCompleteFrom(this.filteredResults(), this.query);
                    if (next === null) {
                        this.flash();
                        return;
                    }
                    this.query = next;
                    this.selectedIndex = 0;
                    this.$nextTick(() => {
                        const input = this.$refs.searchInput;
                        if (!input) return;
                        input.focus();
                        const end = this.query.length;
                        input.setSelectionRange(end, end);
                    });
                } else if (e.key === 'Enter') {
                    e.preventDefault();
                    const result = this.filteredResults()[this.selectedIndex];
                    if (!result) return;
                    // Enter on a prefix suggestion expands the keyword
                    // and waits for the user to type the actual query —
                    // launching a search with no query would be useless.
                    if (result.type === 'engine-prefix') {
                        this.query = autocompleteFor(result, this.query);
                        this.selectedIndex = 0;
                        this.$nextTick(() => {
                            const input = this.$refs.searchInput;
                            if (!input) return;
                            input.focus();
                            const end = this.query.length;
                            input.setSelectionRange(end, end);
                        });
                        return;
                    }
                    this.navigate(result, { metaKey: e.metaKey, ctrlKey: e.ctrlKey });
                } else if (e.key === 'Escape') {
                    this.hide();
                }
            },

            onInput() {
                this.selectedIndex = 0;
            },

            scrollToSelected() {
                this.$nextTick(() => {
                    const container = this.$refs.searchResults;
                    const selected = container?.querySelector('.selected');
                    if (selected) {
                        selected.scrollIntoView({ block: 'nearest' });
                    }
                });
            },

            resultURL(item) {
                if (item.type === 'engine') {
                    return buildEngineURL(item.engine, item.query);
                }
                if (item.type === 'engine-prefix') {
                    // No URL — Enter on this row expands the keyword
                    // instead of navigating. The placeholder href keeps
                    // the link valid for screen readers and right-click.
                    return '#';
                }
                return item.url;
            },

            navigate(item, modifiers) {
                if (item.type === 'engine-prefix') {
                    this.query = autocompleteFor(item, this.query);
                    this.selectedIndex = 0;
                    this.$nextTick(() => {
                        const input = this.$refs.searchInput;
                        if (!input) return;
                        input.focus();
                        const end = this.query.length;
                        input.setSelectionRange(end, end);
                    });
                    return;
                }
                const intent = navigationIntent(item, modifiers || {});
                if (!intent) return;
                if (intent.newTab) {
                    // Keep the modal open so the user can fire several
                    // lookups in a row without losing context.
                    window.open(intent.url, '_blank', 'noopener,noreferrer');
                    return;
                }
                this.open = false;
                this.$nextTick(() => {
                    window.location.href = intent.url;
                });
            },

            iconClass(icon, type) {
                return iconClass(icon, type);
            },

            openHelp() { this.helpOpen = true; },
            closeHelp() { this.helpOpen = false; },
            toggleHelp() { this.helpOpen = !this.helpOpen; },

            quickJump(key) {
                const pages = this.nav.filter(item => item.name && item.name !== 'stats');
                if (key === '0') {
                    const stats = this.nav.find(item => item.name === 'stats');
                    if (stats) window.location.href = stats.url;
                    return;
                }
                const idx = parseInt(key, 10) - 1;
                if (idx >= 0 && idx < pages.length) {
                    window.location.href = pages[idx].url;
                }
            },

            // iconImageURL returns a favicon URL when an engine row has
            // no explicit icon and we haven't already learned the
            // favicon fails to load. The template renders an <img>
            // when this is non-null and falls back to <i> otherwise.
            iconImageURL(item) {
                if (!item || item.icon) return null;
                if (item.type !== 'engine' && item.type !== 'engine-prefix') return null;
                if (!item.engine) return null;
                if (this.faviconFailed[item.engine.name]) return null;
                return engineFaviconURL(item.engine);
            },

            onFaviconError(item) {
                if (item && item.engine) {
                    this.faviconFailed[item.engine.name] = true;
                }
            },

            flash() {
                this.flashing = false;
                if (this._flashTimer) clearTimeout(this._flashTimer);
                // Re-enable on the next tick so the CSS animation
                // restarts even when several flashes fire in quick
                // succession.
                this.$nextTick(() => {
                    this.flashing = true;
                    this._flashTimer = setTimeout(() => {
                        this.flashing = false;
                        this._flashTimer = null;
                    }, 600);
                });
            },

            async initSearch() {
                document.addEventListener('keydown', (e) => {
                    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
                    // Help modal swallows Escape (and ?, which toggles
                    // it off) before the global registry runs so the
                    // dialog can close cleanly.
                    if (this.helpOpen && (e.key === 'Escape' || e.key === '?')) {
                        e.preventDefault();
                        this.closeHelp();
                        return;
                    }
                    matchGlobal(keybindings, this, e);
                });
                try {
                    const [linksResp, enginesResp] = await Promise.all([
                        fetch('api/all-links').then(r => r.json()).catch(() => []),
                        fetch('api/search-engines').then(r => r.json()).catch(() => null),
                    ]);
                    this.allLinks = linksResp || [];
                    if (enginesResp) {
                        this.engines = enginesResp.engines || [];
                        this.defaultEngine = enginesResp.default || '';
                    }
                } catch (e) {
                    console.error('Failed to load search data:', e);
                }
            }
        }));
    });
}

// Config version watcher. Every successful config reload increments
// the version reported at /api/config-errors; once a tab has recorded
// its starting version, any change triggers a hard reload so the
// operator's KDL edits land in every open dashboard automatically.
const CONFIG_POLL_INTERVAL_MS = 5000;

export async function pollConfigVersion(state, deps) {
    const { fetch: f, reload } = deps;
    try {
        const resp = await f('api/config-errors');
        if (!resp || !resp.ok) return state;
        const data = await resp.json();
        if (typeof data.version !== 'number') return state;
        if (state.initial === null) {
            return { initial: data.version };
        }
        if (data.version !== state.initial) {
            reload();
        }
        return state;
    } catch (e) {
        return state;
    }
}

if (typeof window !== 'undefined' && typeof fetch === 'function') {
    let state = { initial: null };
    const deps = {
        fetch: (url) => fetch(url),
        reload: () => window.location.reload(),
    };
    const tick = async () => { state = await pollConfigVersion(state, deps); };
    tick();
    setInterval(tick, CONFIG_POLL_INTERVAL_MS);
}

// Keyboard shortcut registry. Single source of truth for both the
// global keydown dispatcher and the `?` legend modal — every binding
// the dashboard advertises lives here. Each entry has:
//   keys        — display labels (and, for global scope, the actual
//                 KeyboardEvent.key values to match against).
//   scope       — 'global' (page-level keydown), 'search' (only
//                 meaningful while the search modal has focus), or
//                 'help' (active while the legend is open).
//   description — one-line copy shown in the legend.
//   handler     — required for scope='global'; invoked with the
//                 Alpine search component as `c` and the event.
export const keybindings = [
    {
        keys: ['/'],
        scope: 'global',
        description: 'Open the search palette',
        handler: (c, e) => { e.preventDefault(); c.show(); },
    },
    {
        keys: ['?'],
        scope: 'global',
        description: 'Show this keyboard shortcut legend',
        handler: (c, e) => { e.preventDefault(); c.toggleHelp(); },
    },
    {
        keys: ['0'],
        scope: 'global',
        description: 'Jump to the Statistics page',
        handler: (c) => { c.quickJump('0'); },
    },
    {
        keys: ['1', '2', '3', '4', '5', '6', '7', '8', '9'],
        scope: 'global',
        description: 'Jump to the n-th page in the nav menu',
        handler: (c, e) => { c.quickJump(e.key); },
    },
    {
        keys: ['Esc'],
        scope: 'search',
        description: 'Close the search palette',
    },
    {
        keys: ['↑', '↓'],
        scope: 'search',
        description: 'Move the selection in the search results',
    },
    {
        keys: ['Tab'],
        scope: 'search',
        description: 'Autocomplete to the longest unambiguous prefix; flash if ambiguous',
    },
    {
        keys: ['↵'],
        scope: 'search',
        description: 'Open the selected result',
    },
    {
        keys: ['⌘↵', 'Ctrl+↵'],
        scope: 'search',
        description: 'Open the selected result in a new tab; modal stays open',
    },
    {
        keys: ['Esc', '?'],
        scope: 'help',
        description: 'Close this legend',
    },
];

// matchGlobal tries every global binding against the event and, on a
// hit, dispatches the handler. Returns true when something matched so
// the caller can stop propagating.
export function matchGlobal(bindings, c, e) {
    for (const b of bindings) {
        if (b.scope !== 'global') continue;
        if (!b.keys.includes(e.key)) continue;
        b.handler(c, e);
        return true;
    }
    return false;
}

// groupBindingsForLegend returns the registry grouped by scope, in the
// order rendered in the legend modal: global → search → help.
export function groupBindingsForLegend(bindings) {
    const order = ['global', 'search', 'help'];
    const labels = {
        global: 'Anywhere',
        search: 'In the search palette',
        help: 'In this legend',
    };
    return order.map(scope => ({
        scope,
        label: labels[scope],
        entries: bindings.filter(b => b.scope === scope),
    })).filter(g => g.entries.length > 0);
}

// Theme handling. Saved preference takes precedence over the system
// pref so a user who explicitly opted into one mode keeps it across
// reloads. The choice is persisted in a cookie scoped to the parent
// domain so it survives navigation between pages.subspace.pub and
// stats.subspace.pub (which are separate origins as far as the
// browser is concerned, so localStorage would not cross).
const THEME_COOKIE_NAME = 'subspace-theme';
const THEME_COOKIE_DOMAIN = '.subspace.pub';
const THEME_COOKIE_MAX_AGE = 60 * 60 * 24 * 365; // one year

export function resolveTheme(saved, systemPrefersDark) {
    if (saved === 'light' || saved === 'dark') return saved;
    return systemPrefersDark ? 'dark' : 'light';
}

export function nextTheme(current) {
    return current === 'light' ? 'dark' : 'light';
}

// parseCookies extracts a name → value map from a `document.cookie`
// string. Pure so it can be tested without a DOM.
export function parseCookies(cookieString) {
    const out = {};
    if (!cookieString) return out;
    for (const part of cookieString.split(';')) {
        const trimmed = part.trim();
        if (!trimmed) continue;
        const eq = trimmed.indexOf('=');
        if (eq === -1) continue;
        const key = trimmed.slice(0, eq);
        let val;
        try {
            val = decodeURIComponent(trimmed.slice(eq + 1));
        } catch (_) {
            val = trimmed.slice(eq + 1);
        }
        out[key] = val;
    }
    return out;
}

// buildThemeCookie returns the Set-Cookie-style string written to
// document.cookie when persisting a theme choice. Pure so it can be
// asserted against in tests.
export function buildThemeCookie(theme, { domain, maxAge } = {}) {
    const parts = [
        THEME_COOKIE_NAME + '=' + encodeURIComponent(theme),
        'Path=/',
        'Max-Age=' + (maxAge != null ? maxAge : THEME_COOKIE_MAX_AGE),
        'SameSite=Lax',
    ];
    const dom = domain != null ? domain : THEME_COOKIE_DOMAIN;
    if (dom) parts.push('Domain=' + dom);
    return parts.join('; ');
}

if (typeof document !== 'undefined') {
    const mql = typeof window.matchMedia === 'function'
        ? window.matchMedia('(prefers-color-scheme: dark)')
        : null;

    const apply = (theme) => {
        document.documentElement.setAttribute('data-theme', theme);
    };

    const readSaved = () => {
        const cookies = parseCookies(document.cookie);
        if (cookies[THEME_COOKIE_NAME] === 'light' || cookies[THEME_COOKIE_NAME] === 'dark') {
            return cookies[THEME_COOKIE_NAME];
        }
        // Fall back to localStorage for environments that block
        // cross-subdomain cookies, or for legacy data written by an
        // earlier build.
        try { return localStorage.getItem(THEME_COOKIE_NAME); }
        catch (_) { return null; }
    };

    apply(resolveTheme(readSaved(), mql ? mql.matches : true));

    // Follow the system theme when no explicit choice has been saved.
    if (mql && typeof mql.addEventListener === 'function') {
        mql.addEventListener('change', (e) => {
            const cur = readSaved();
            if (cur !== 'light' && cur !== 'dark') {
                apply(e.matches ? 'dark' : 'light');
            }
        });
    }

    // Expose a tiny global so the inline header button can call it
    // without needing Alpine state on every page.
    window.subspaceToggleTheme = function () {
        const cur = document.documentElement.getAttribute('data-theme') || 'dark';
        const next = nextTheme(cur);
        apply(next);
        document.cookie = buildThemeCookie(next, {});
        try { localStorage.setItem(THEME_COOKIE_NAME, next); } catch (_) {}
        // Notify any listeners (e.g. Chart.js renderers) that need
        // to re-render with the new theme colors.
        window.dispatchEvent(new CustomEvent('subspace:themechange', { detail: { theme: next } }));
    };
}
