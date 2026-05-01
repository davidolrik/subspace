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

// buildEngineURL substitutes the user's query into an engine's URL
// template by replacing every "{query}" with the URL-encoded query.
export function buildEngineURL(engine, query) {
    return engine.url.replace(/\{query\}/g, encodeURIComponent(query));
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

    // Fallback: when nothing surfaced — no exact keyword, no engine
    // prefix suggestions, no local matches — route the full query
    // through the configured default engine so the user always has a
    // destination. Skip this when prefix rows already populated the
    // list, since those are the discoverability path.
    const hasPrefixRows = out.some(r => r.type === 'engine-prefix');
    if (!keywordMatched && !hasPrefixRows && pages.length === 0 && links.length === 0 && defaultEngine) {
        const target = defaultEngine.toLowerCase();
        const def = (engines || []).find(e => e.name.toLowerCase() === target);
        if (def) {
            out.push({
                type: 'engine',
                engine: def,
                query: trimmed,
                label: 'Search ' + def.name + ' for "' + trimmed + '"',
                icon: def.icon,
                meta: '',
            });
        }
    }

    return out;
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
    const top = results[0];
    if (top && top.type === 'engine') {
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
                    this.navigate(result);
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

            navigate(item) {
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
                this.open = false;
                const url = this.resultURL(item);
                this.$nextTick(() => {
                    window.location.href = url;
                });
            },

            iconClass(icon, type) {
                return iconClass(icon, type);
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
                    if (e.key === '/') {
                        e.preventDefault();
                        this.show();
                        return;
                    }
                    // Quick navigation: 1-9 for pages, 0 for statistics
                    if (e.key >= '0' && e.key <= '9') {
                        const pages = this.nav.filter(
                            item => item.name && item.name !== 'stats'
                        );
                        if (e.key === '0') {
                            const stats = this.nav.find(
                                item => item.name === 'stats'
                            );
                            if (stats) window.location.href = stats.url;
                        } else {
                            const idx = parseInt(e.key, 10) - 1;
                            if (idx < pages.length) {
                                window.location.href = pages[idx].url;
                            }
                        }
                        return;
                    }
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
