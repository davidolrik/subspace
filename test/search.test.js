import { describe, it, expect, vi } from 'vitest';
import {
    matchEngine,
    matchEnginePrefixes,
    buildEngineURL,
    buildResults,
    autocompleteFor,
    commonPrefix,
    tabCompleteFrom,
    navigationIntent,
    engineFaviconURL,
    pollConfigVersion,
    resolveTheme,
    nextTheme,
    parseCookies,
    buildThemeCookie,
    keybindings,
    matchGlobal,
    groupBindingsForLegend,
    bands,
} from '../pages/frontend/search.js';

const engines = [
    { name: 'google', alias: 'g', url: 'https://www.google.com/search?q={query}', icon: 'si-google' },
    { name: 'metacpan', alias: 'cpan', url: 'https://metacpan.org/search?q={query}', icon: 'fa-cube' },
];

const nav = [
    { label: 'Development', url: 'http://pages.subspace.pub/dev/', name: 'dev', alias: 'd', icon: 'fa-code' },
    { label: 'Operations', url: 'http://pages.subspace.pub/ops/', name: 'ops', alias: '', icon: 'fa-server' },
];

const allLinks = [
    { name: 'ojo', url: 'https://metacpan.org/pod/ojo', icon: 'fa-cube', section: 'Docs', page: 'Development' },
    { name: 'GitHub', url: 'https://github.com/davidolrik/subspace', icon: 'si-github', section: 'Repos', page: 'Development' },
];

describe('matchEngine', () => {
    it('hits by name', () => {
        expect(matchEngine(engines, 'google')?.name).toBe('google');
    });

    it('hits by alias', () => {
        expect(matchEngine(engines, 'cpan')?.name).toBe('metacpan');
    });

    it('is case-insensitive', () => {
        expect(matchEngine(engines, 'GOOGLE')?.name).toBe('google');
        expect(matchEngine(engines, 'CpAn')?.name).toBe('metacpan');
    });

    it('returns undefined for unknown tokens', () => {
        expect(matchEngine(engines, 'nope')).toBeUndefined();
    });

    it('returns undefined for empty token', () => {
        expect(matchEngine(engines, '')).toBeUndefined();
    });
});

describe('matchEnginePrefixes', () => {
    it('returns engines whose name starts with the prefix', () => {
        const r = matchEnginePrefixes(engines, 'goo');
        expect(r.map(e => e.name)).toEqual(['google']);
    });

    it('returns engines whose alias starts with the prefix', () => {
        const r = matchEnginePrefixes(engines, 'cp');
        expect(r.map(e => e.name)).toEqual(['metacpan']);
    });

    it('is case-insensitive', () => {
        expect(matchEnginePrefixes(engines, 'CP').map(e => e.name)).toEqual(['metacpan']);
    });

    it('skips engines that match exactly (already emitted as the keyword row)', () => {
        expect(matchEnginePrefixes(engines, 'cpan')).toEqual([]);
        expect(matchEnginePrefixes(engines, 'g')).toEqual([]);
    });

    it('returns nothing for empty token', () => {
        expect(matchEnginePrefixes(engines, '')).toEqual([]);
    });
});

describe('buildEngineURL', () => {
    it('replaces {query} with the URL-encoded query', () => {
        expect(buildEngineURL(engines[0], 'hello world'))
            .toBe('https://www.google.com/search?q=hello%20world');
    });

    it('replaces every occurrence of {query}', () => {
        const e = { name: 'x', url: 'https://x.example/?a={query}&b={query}' };
        expect(buildEngineURL(e, 'foo')).toBe('https://x.example/?a=foo&b=foo');
    });

    it('encodes special characters', () => {
        expect(buildEngineURL(engines[0], 'a&b=c'))
            .toBe('https://www.google.com/search?q=a%26b%3Dc');
    });

    it('replaces {query} in both query string and fragment (urlscan-style)', () => {
        const e = { name: 'urlscan', url: 'https://urlscan.io/search/?q={query}#{query}' };
        expect(buildEngineURL(e, 'google.com'))
            .toBe('https://urlscan.io/search/?q=google.com#google.com');
    });

    it('defaults to component encoding (spaces become %20)', () => {
        const e = { url: 'https://x.example/?q={query}' };
        expect(buildEngineURL(e, 'hello world')).toBe('https://x.example/?q=hello%20world');
    });

    it('honours url-encode="component" explicitly', () => {
        const e = { url: 'https://x.example/?q={query}', urlEncode: 'component' };
        expect(buildEngineURL(e, 'hello world')).toBe('https://x.example/?q=hello%20world');
    });

    it('uses + for spaces under url-encode="form"', () => {
        const e = { url: 'https://x.example/?q={query}', urlEncode: 'form' };
        expect(buildEngineURL(e, 'hello world')).toBe('https://x.example/?q=hello+world');
    });

    it('still URL-encodes other special chars under form mode', () => {
        const e = { url: 'https://x.example/?q={query}', urlEncode: 'form' };
        // & and = must still be encoded — only space gets the + treatment.
        expect(buildEngineURL(e, 'a&b=c d')).toBe('https://x.example/?q=a%26b%3Dc+d');
    });

    it('passes the query through verbatim under url-encode="raw"', () => {
        const e = { url: 'https://internal/q?{query}', urlEncode: 'raw' };
        // Raw mode is for engines whose URL grammar is encoded differently
        // by the caller (e.g. embedding pre-built query strings). The
        // dashboard does no transformation at all.
        expect(buildEngineURL(e, 'hello world&foo=bar'))
            .toBe('https://internal/q?hello world&foo=bar');
    });
});

describe('buildResults', () => {
    it('returns nav rows when query is empty', () => {
        const rows = buildResults({ query: '', nav, allLinks, engines, defaultEngine: 'google' });
        expect(rows).toHaveLength(nav.length);
        expect(rows[0].type).toBe('page');
        expect(rows[0].label).toBe('Development');
    });

    it('emits an engine row plus local matches when keyword + tail match', () => {
        const rows = buildResults({
            query: 'cpan ojo',
            nav,
            allLinks,
            engines,
            defaultEngine: 'google',
        });
        expect(rows[0].type).toBe('engine');
        expect(rows[0].engine.name).toBe('metacpan');
        expect(rows[0].label).toBe('Search metacpan for "ojo"');
        const labels = rows.map(r => r.label);
        expect(labels).toContain('ojo');
    });

    it('keyword without trailing query still emits an engine row with empty query', () => {
        const rows = buildResults({
            query: 'cpan',
            nav,
            allLinks,
            engines,
            defaultEngine: 'google',
        });
        expect(rows[0].type).toBe('engine');
        expect(rows[0].engine.name).toBe('metacpan');
        expect(rows[0].query).toBe('');
    });

    it('surfaces engine-prefix rows when the partial first token matches', () => {
        const rows = buildResults({
            query: 'cp',
            nav,
            allLinks,
            engines,
            defaultEngine: 'google',
        });
        const prefixRows = rows.filter(r => r.type === 'engine-prefix');
        expect(prefixRows).toHaveLength(1);
        expect(prefixRows[0].engine.name).toBe('metacpan');
        // The default-engine fallback must not also fire when prefix
        // suggestions populated the list.
        const fallback = rows.find(r => r.type === 'engine');
        expect(fallback).toBeUndefined();
    });

    it('does not emit prefix rows once the user has typed a space (commits to the keyword)', () => {
        const rows = buildResults({
            query: 'cp ',
            nav,
            allLinks,
            engines,
            defaultEngine: 'google',
        });
        expect(rows.some(r => r.type === 'engine-prefix')).toBe(false);
    });

    it('emits a single default-engine fallback row when nothing else matches', () => {
        const rows = buildResults({
            query: 'xyzzy-nonexistent',
            nav,
            allLinks,
            engines,
            defaultEngine: 'google',
        });
        const types = rows.map(r => r.type);
        expect(types).toEqual(['engine']);
        expect(rows[0].engine.name).toBe('google');
        expect(rows[0].query).toBe('xyzzy-nonexistent');
    });

    it('does not emit a fallback when defaultEngine is unset', () => {
        const rows = buildResults({
            query: 'xyzzy-nonexistent',
            nav,
            allLinks,
            engines,
            defaultEngine: '',
        });
        expect(rows).toHaveLength(0);
    });

    it('surfaces the engine description on exact-keyword rows', () => {
        const richEngines = [
            { name: 'metacpan', alias: 'cpan', url: 'https://metacpan.org/search?q={query}', icon: 'fa-cube', description: 'Perl module index' },
        ];
        const rows = buildResults({
            query: 'cpan ojo',
            nav: [],
            allLinks: [],
            engines: richEngines,
            defaultEngine: '',
        });
        expect(rows[0].type).toBe('engine');
        expect(rows[0].description).toBe('Perl module index');
    });

    it('surfaces the engine description on prefix suggestion rows', () => {
        const richEngines = [
            { name: 'metacpan', alias: 'cpan', url: 'https://metacpan.org/search?q={query}', description: 'Perl module index' },
        ];
        const rows = buildResults({
            query: 'cp',
            nav: [],
            allLinks: [],
            engines: richEngines,
            defaultEngine: '',
        });
        expect(rows[0].type).toBe('engine-prefix');
        expect(rows[0].description).toBe('Perl module index');
    });

    it('surfaces the engine description on the default-engine fallback row', () => {
        const richEngines = [
            { name: 'google', url: 'https://www.google.com/search?q={query}', description: 'General web search' },
        ];
        const rows = buildResults({
            query: 'xyzzy-nonexistent',
            nav: [],
            allLinks: [],
            engines: richEngines,
            defaultEngine: 'google',
        });
        expect(rows).toHaveLength(1);
        expect(rows[0].description).toBe('General web search');
    });

    it('renders one fallback row per engine with fallback=true plus the default', () => {
        const richEngines = [
            { name: 'google',  url: 'https://www.google.com/search?q={query}' },
            { name: 'kagi',    url: 'https://kagi.com/search?q={query}',       fallback: true },
            { name: 'urlscan', url: 'https://urlscan.io/search/?q={query}' },
            { name: 'ddg',     url: 'https://duckduckgo.com/?q={query}',       fallback: true },
        ];
        const rows = buildResults({
            query: 'xyzzy-nonexistent',
            nav: [],
            allLinks: [],
            engines: richEngines,
            defaultEngine: 'google',
        });
        // Expect: default first (google), then opt-ins alphabetically (ddg, kagi).
        // urlscan is keyword-only — never appears in fallback list.
        const names = rows.map(r => r.engine.name);
        expect(names).toEqual(['google', 'ddg', 'kagi']);
        expect(names).not.toContain('urlscan');
    });

    it('does not duplicate the default engine when it also has fallback=true', () => {
        const richEngines = [
            { name: 'google', url: 'https://www.google.com/search?q={query}', fallback: true },
            { name: 'kagi',   url: 'https://kagi.com/search?q={query}',       fallback: true },
        ];
        const rows = buildResults({
            query: 'xyzzy',
            nav: [],
            allLinks: [],
            engines: richEngines,
            defaultEngine: 'google',
        });
        const names = rows.map(r => r.engine.name);
        expect(names).toEqual(['google', 'kagi']);
    });

    it('renders fallback engines even when no default is configured', () => {
        const richEngines = [
            { name: 'kagi', url: 'https://kagi.com/search?q={query}', fallback: true },
            { name: 'ddg',  url: 'https://duckduckgo.com/?q={query}', fallback: true },
        ];
        const rows = buildResults({
            query: 'xyzzy',
            nav: [],
            allLinks: [],
            engines: richEngines,
            defaultEngine: '',
        });
        const names = rows.map(r => r.engine.name);
        expect(names).toEqual(['ddg', 'kagi']);
    });

    it('renders nothing when no default and no fallback engines are configured', () => {
        const richEngines = [
            { name: 'urlscan', url: 'https://urlscan.io/search/?q={query}' },
        ];
        const rows = buildResults({
            query: 'xyzzy',
            nav: [],
            allLinks: [],
            engines: richEngines,
            defaultEngine: '',
        });
        expect(rows).toHaveLength(0);
    });

    it('matches the default engine case-insensitively', () => {
        const casedEngines = [
            { name: 'Google', url: 'https://www.google.com/search?q={query}' },
        ];
        const rows = buildResults({
            query: 'xyzzy-nonexistent',
            nav: [],
            allLinks: [],
            engines: casedEngines,
            defaultEngine: 'google',
        });
        expect(rows).toHaveLength(1);
        expect(rows[0].type).toBe('engine');
        // Casing of the displayed name is preserved from the engine
        // definition, not from the (lowercased) default-engine field.
        expect(rows[0].engine.name).toBe('Google');
    });

    it('appends fallback engines after local matches so the user can still pivot to web search', () => {
        // Mirrors the real-world case: the user types "test" and gets
        // a Pages/Links section. The fallback engine rows should still
        // trail at the bottom so they don't have to retype to search
        // the web.
        const rows = buildResults({
            query: 'GitHub',
            nav,
            allLinks,
            engines,
            defaultEngine: 'google',
        });
        // Local match comes first.
        const types = rows.map(r => r.type);
        const firstLink = types.indexOf('link');
        const firstFallback = rows.findIndex(r => r.type === 'engine' && r.fallback);
        expect(firstLink).toBeGreaterThanOrEqual(0);
        expect(firstFallback).toBeGreaterThan(firstLink);
        // The fallback row exists and points at the default engine.
        expect(rows[firstFallback].engine.name).toBe('google');
    });
});

describe('autocompleteFor', () => {
    it('completes engine row to "<keyword> " preserving any trailing query', () => {
        const result = { type: 'engine', engine: { name: 'metacpan' } };
        expect(autocompleteFor(result, 'cpan')).toBe('metacpan ');
        expect(autocompleteFor(result, 'cp ojo')).toBe('metacpan ojo');
        expect(autocompleteFor(result, 'cpan ojo extra')).toBe('metacpan ojo extra');
    });

    it('expands an engine-prefix row to the full engine name', () => {
        const result = { type: 'engine-prefix', engine: { name: 'metacpan' } };
        expect(autocompleteFor(result, 'cp')).toBe('metacpan ');
    });

    it('completes page/link row to its label', () => {
        const result = { type: 'page', label: 'Development' };
        expect(autocompleteFor(result, 'dev')).toBe('Development');
    });
});

describe('commonPrefix', () => {
    it('returns the input for a single string', () => {
        expect(commonPrefix(['Dashboard'])).toBe('Dashboard');
    });

    it('finds the shared prefix case-insensitively, preserving the first string\'s case', () => {
        expect(commonPrefix(['Dashboard', 'Dagobah'])).toBe('Da');
        expect(commonPrefix(['dashboard', 'Dagobah'])).toBe('da');
    });

    it('returns empty when there is no shared prefix', () => {
        expect(commonPrefix(['Dashboard', 'Operations'])).toBe('');
    });

    it('returns empty for empty input', () => {
        expect(commonPrefix([])).toBe('');
    });
});

describe('tabCompleteFrom', () => {
    it('returns null when results is empty (caller flashes)', () => {
        expect(tabCompleteFrom([], 'da')).toBeNull();
    });

    it('returns null when the LCP is no longer than what the user already typed', () => {
        const results = [
            { type: 'page', label: 'Dashboard' },
            { type: 'page', label: 'Dagobah' },
        ];
        expect(tabCompleteFrom(results, 'da')).toBeNull();
        expect(tabCompleteFrom(results, 'Da')).toBeNull();
    });

    it('extends the input to the LCP when it grows beyond the current query', () => {
        const results = [
            { type: 'page', label: 'Dashboard' },
            { type: 'page', label: 'Database' },
        ];
        expect(tabCompleteFrom(results, 'd')).toBe('Da');
    });

    it('completes a single page row to its full label', () => {
        const results = [{ type: 'page', label: 'Development' }];
        expect(tabCompleteFrom(results, 'dev')).toBe('Development');
    });

    it('appends a trailing space when a single engine-prefix row matches', () => {
        const results = [{ type: 'engine-prefix', engine: { name: 'metacpan' } }];
        expect(tabCompleteFrom(results, 'cp')).toBe('metacpan ');
    });

    it('does not append a trailing space when multiple engine-prefix rows share a prefix', () => {
        const results = [
            { type: 'engine-prefix', engine: { name: 'github' } },
            { type: 'engine-prefix', engine: { name: 'gitlab' } },
        ];
        expect(tabCompleteFrom(results, 'g')).toBe('git');
    });

    it('returns null when the prefix mixes engines and pages with no shared extension', () => {
        const results = [
            { type: 'engine-prefix', engine: { name: 'google' } },
            { type: 'page', label: 'Grafana' },
        ];
        expect(tabCompleteFrom(results, 'g')).toBeNull();
    });

    it('ignores page/link rows whose label does not start with the query (secondary-field matches)', () => {
        // Mirrors the real-world case: typing "das" surfaces the
        // Dashboard page itself, plus several links that landed in the
        // results because their `page` field is "Dashboard". Only the
        // page row should count as a completion candidate.
        const results = [
            { type: 'page',  label: 'Dashboard' },
            { type: 'link',  label: 'Dagobah' },
            { type: 'link',  label: 'pve01' },
            { type: 'link',  label: 'Home Assistant' },
        ];
        expect(tabCompleteFrom(results, 'das')).toBe('Dashboard');
    });

    it('does not treat a fallback row as an exact-keyword candidate', () => {
        // The fallback row exists because nothing matched, not because
        // the user typed "google" — pressing Tab here should not turn
        // their query into "google <whatever>". With no other
        // candidates the function returns null, signalling a flash.
        const results = [
            { type: 'engine', engine: { name: 'google' }, query: 'xyzzy', fallback: true },
        ];
        expect(tabCompleteFrom(results, 'xyzzy')).toBeNull();
    });

    it('preserves keyword expansion when the top result is an exact engine match', () => {
        const results = [
            { type: 'engine', engine: { name: 'metacpan' }, query: 'ojo' },
            { type: 'link', label: 'ojo' },
        ];
        expect(tabCompleteFrom(results, 'cpan ojo')).toBe('metacpan ojo');
    });
});

describe('engineFaviconURL', () => {
    it('points at the dashboard favicon proxy keyed by engine host', () => {
        const e = { url: 'https://www.google.com/search?q={query}' };
        expect(engineFaviconURL(e)).toBe('api/favicon?host=www.google.com');
    });

    it('routes http engines through the same proxy', () => {
        const e = { url: 'http://internal.example.com/?q={query}' };
        expect(engineFaviconURL(e)).toBe('api/favicon?host=internal.example.com');
    });

    it('handles URLs with {query} in the fragment', () => {
        const e = { url: 'https://urlscan.io/search/?q={query}#{query}' };
        expect(engineFaviconURL(e)).toBe('api/favicon?host=urlscan.io');
    });

    it('URL-encodes hosts with non-default ports', () => {
        const e = { url: 'https://search.example.com:8443/?q={query}' };
        expect(engineFaviconURL(e)).toBe('api/favicon?host=search.example.com%3A8443');
    });

    it('returns null for engines without a URL', () => {
        expect(engineFaviconURL({})).toBeNull();
        expect(engineFaviconURL(null)).toBeNull();
        expect(engineFaviconURL(undefined)).toBeNull();
    });

    it('returns null for unparseable URLs', () => {
        expect(engineFaviconURL({ url: 'not a url' })).toBeNull();
    });
});

describe('navigationIntent', () => {
    it('returns plain navigation for a page row with no modifier', () => {
        const result = { type: 'page', url: 'http://example.com/dev/' };
        expect(navigationIntent(result, {})).toEqual({ url: 'http://example.com/dev/', newTab: false });
    });

    it('returns new-tab navigation when metaKey is held (Cmd on macOS)', () => {
        const result = { type: 'link', url: 'https://github.com' };
        expect(navigationIntent(result, { metaKey: true })).toEqual({ url: 'https://github.com', newTab: true });
    });

    it('returns new-tab navigation when ctrlKey is held (Linux/Windows)', () => {
        const result = { type: 'link', url: 'https://github.com' };
        expect(navigationIntent(result, { ctrlKey: true })).toEqual({ url: 'https://github.com', newTab: true });
    });

    it('builds the engine URL on demand for engine rows', () => {
        const result = {
            type: 'engine',
            engine: { name: 'metacpan', url: 'https://metacpan.org/search?q={query}' },
            query: 'ojo',
        };
        expect(navigationIntent(result, { metaKey: true }))
            .toEqual({ url: 'https://metacpan.org/search?q=ojo', newTab: true });
    });

    it('returns null for engine-prefix rows regardless of modifier', () => {
        const result = { type: 'engine-prefix', engine: { name: 'metacpan' } };
        expect(navigationIntent(result, {})).toBeNull();
        expect(navigationIntent(result, { metaKey: true })).toBeNull();
    });

    it('returns null when no result is supplied', () => {
        expect(navigationIntent(null, {})).toBeNull();
        expect(navigationIntent(undefined, { ctrlKey: true })).toBeNull();
    });
});

describe('pollConfigVersion', () => {
    function makeDeps(version, { ok = true } = {}) {
        let reloaded = false;
        const deps = {
            fetch: async () => ({
                ok,
                json: async () => ({ errors: [], version }),
            }),
            reload: () => { reloaded = true; },
        };
        return { deps, didReload: () => reloaded };
    }

    it('records the initial version on first poll without reloading', async () => {
        const { deps, didReload } = makeDeps(7);
        const state = await pollConfigVersion({ initial: null }, deps);
        expect(state.initial).toBe(7);
        expect(didReload()).toBe(false);
    });

    it('does not reload when the version is unchanged', async () => {
        const { deps, didReload } = makeDeps(7);
        await pollConfigVersion({ initial: 7 }, deps);
        expect(didReload()).toBe(false);
    });

    it('reloads when the backend version has changed', async () => {
        const { deps, didReload } = makeDeps(8);
        await pollConfigVersion({ initial: 7 }, deps);
        expect(didReload()).toBe(true);
    });

    it('keeps state and skips reload on a network error', async () => {
        let reloaded = false;
        const deps = {
            fetch: async () => { throw new Error('offline'); },
            reload: () => { reloaded = true; },
        };
        const state = await pollConfigVersion({ initial: 7 }, deps);
        expect(state.initial).toBe(7);
        expect(reloaded).toBe(false);
    });

    it('keeps state and skips reload on a non-OK response', async () => {
        const { deps, didReload } = makeDeps(8, { ok: false });
        const state = await pollConfigVersion({ initial: 7 }, deps);
        expect(state.initial).toBe(7);
        expect(didReload()).toBe(false);
    });
});

describe('resolveTheme', () => {
    it('honours an explicit saved light preference', () => {
        expect(resolveTheme('light', true)).toBe('light');
        expect(resolveTheme('light', false)).toBe('light');
    });

    it('honours an explicit saved dark preference', () => {
        expect(resolveTheme('dark', true)).toBe('dark');
        expect(resolveTheme('dark', false)).toBe('dark');
    });

    it('falls back to the system preference when nothing is saved', () => {
        expect(resolveTheme(null, true)).toBe('dark');
        expect(resolveTheme(null, false)).toBe('light');
        expect(resolveTheme('', true)).toBe('dark');
        expect(resolveTheme(undefined, false)).toBe('light');
    });

    it('treats unrecognised saved values as if no preference was saved', () => {
        expect(resolveTheme('blue', true)).toBe('dark');
        expect(resolveTheme('blue', false)).toBe('light');
    });
});

describe('nextTheme', () => {
    it('toggles light to dark and dark to light', () => {
        expect(nextTheme('light')).toBe('dark');
        expect(nextTheme('dark')).toBe('light');
    });

    it('treats anything other than "light" as dark and toggles to light', () => {
        expect(nextTheme('')).toBe('light');
        expect(nextTheme('whatever')).toBe('light');
    });
});

describe('parseCookies', () => {
    it('parses a typical document.cookie string', () => {
        const got = parseCookies('a=1; b=two; subspace-theme=light');
        expect(got).toEqual({ a: '1', b: 'two', 'subspace-theme': 'light' });
    });

    it('decodes URL-encoded values', () => {
        const got = parseCookies('greeting=hello%20world');
        expect(got.greeting).toBe('hello world');
    });

    it('returns an empty object for empty / undefined input', () => {
        expect(parseCookies('')).toEqual({});
        expect(parseCookies(undefined)).toEqual({});
    });

    it('skips malformed segments', () => {
        const got = parseCookies('a=1; brokenpair; b=2');
        expect(got).toEqual({ a: '1', b: '2' });
    });
});

describe('keybindings registry', () => {
    it('every entry has at least one key, a scope, and a description', () => {
        for (const b of keybindings) {
            expect(Array.isArray(b.keys), 'keys is array').toBe(true);
            expect(b.keys.length, 'keys non-empty').toBeGreaterThan(0);
            expect(['global', 'search', 'help']).toContain(b.scope);
            expect(typeof b.description).toBe('string');
            expect(b.description.length).toBeGreaterThan(0);
        }
    });

    it('every global binding has a callable handler', () => {
        for (const b of keybindings) {
            if (b.scope !== 'global') continue;
            expect(typeof b.handler, `handler for ${b.keys.join(',')}`).toBe('function');
        }
    });

    it('advertises the canonical entry points (/ and ?)', () => {
        const keys = keybindings.flatMap(b => b.keys);
        expect(keys).toContain('/');
        expect(keys).toContain('?');
    });
});

describe('matchGlobal', () => {
    it('dispatches the handler whose keys list contains the event key', () => {
        const calls = [];
        const fakeBindings = [
            { keys: ['/'], scope: 'global', description: 'open', handler: (c, e) => calls.push(['slash', c, e]) },
            { keys: ['?'], scope: 'global', description: 'help', handler: (c, e) => calls.push(['help', c, e]) },
        ];
        const c = {};
        const ok = matchGlobal(fakeBindings, c, { key: '?' });
        expect(ok).toBe(true);
        expect(calls).toHaveLength(1);
        expect(calls[0][0]).toBe('help');
        expect(calls[0][1]).toBe(c);
    });

    it('returns false when nothing matches', () => {
        const fakeBindings = [
            { keys: ['/'], scope: 'global', description: 'open', handler: () => {} },
        ];
        expect(matchGlobal(fakeBindings, {}, { key: 'q' })).toBe(false);
    });

    it('skips non-global bindings', () => {
        const handler = vi.fn();
        const fakeBindings = [
            { keys: ['Escape'], scope: 'search', description: 'close', handler },
        ];
        expect(matchGlobal(fakeBindings, {}, { key: 'Escape' })).toBe(false);
        expect(handler).not.toHaveBeenCalled();
    });

    it('routes 1-9 to a single multi-key binding', () => {
        const calls = [];
        const fakeBindings = [
            {
                keys: ['1', '2', '3', '4', '5', '6', '7', '8', '9'],
                scope: 'global',
                description: 'jump',
                handler: (c, e) => calls.push(e.key),
            },
        ];
        for (const k of ['1', '5', '9']) {
            matchGlobal(fakeBindings, {}, { key: k });
        }
        expect(calls).toEqual(['1', '5', '9']);
    });
});

describe('groupBindingsForLegend', () => {
    it('groups entries by scope in display order', () => {
        const groups = groupBindingsForLegend(keybindings);
        const scopes = groups.map(g => g.scope);
        expect(scopes).toEqual(['global', 'search', 'help']);
        // Each group has at least one entry.
        for (const g of groups) {
            expect(g.entries.length).toBeGreaterThan(0);
            expect(typeof g.label).toBe('string');
        }
    });

    it('omits groups with no entries', () => {
        const fake = [{ keys: ['/'], scope: 'global', description: 'open', handler: () => {} }];
        const groups = groupBindingsForLegend(fake);
        expect(groups.map(g => g.scope)).toEqual(['global']);
    });
});

describe('buildThemeCookie', () => {
    it('scopes the cookie to the parent domain by default', () => {
        const got = buildThemeCookie('light');
        expect(got).toContain('subspace-theme=light');
        expect(got).toContain('Domain=.subspace.pub');
        expect(got).toContain('Path=/');
        expect(got).toContain('SameSite=Lax');
        expect(got).toMatch(/Max-Age=\d+/);
    });

    it('omits the Domain attribute when explicitly null', () => {
        const got = buildThemeCookie('dark', { domain: '' });
        expect(got).not.toContain('Domain=');
        expect(got).toContain('subspace-theme=dark');
    });

    it('honours a custom Max-Age', () => {
        expect(buildThemeCookie('light', { maxAge: 60 })).toContain('Max-Age=60');
    });
});

describe('bands', () => {
    const list = (name) => ({ Kind: 'list', Section: { Name: name, Links: [], Items: [] } });
    const md = (cols, rows, html, float = '') => ({ Kind: 'markdown', Markdown: { Columns: cols, Rows: rows, Float: float, HTML: html } });

    it('returns an empty array for an empty input', () => {
        expect(bands([])).toEqual([]);
        expect(bands(undefined)).toEqual([]);
    });

    it('wraps a single band markdown into one band', () => {
        const out = bands([md(0, 0, '<p>hi</p>')]);
        expect(out).toEqual([{ kind: 'markdown', html: '<p>hi</p>' }]);
    });

    it('groups consecutive lists into one grid band', () => {
        const out = bands([list('A'), list('B')]);
        expect(out).toHaveLength(1);
        expect(out[0].kind).toBe('grid');
        expect(out[0].cells).toHaveLength(2);
        expect(out[0].cells.map(c => c.kind)).toEqual(['list', 'list']);
    });

    it('splits the grid on a band markdown', () => {
        const out = bands([
            list('A'),
            list('B'),
            md(0, 0, '<p>break</p>'),
            list('C'),
        ]);
        expect(out).toHaveLength(3);
        expect(out[0]).toMatchObject({ kind: 'grid' });
        expect(out[0].cells).toHaveLength(2);
        expect(out[1]).toEqual({ kind: 'markdown', html: '<p>break</p>' });
        expect(out[2]).toMatchObject({ kind: 'grid' });
        expect(out[2].cells).toHaveLength(1);
    });

    it('keeps grid-card markdowns inside the surrounding grid', () => {
        const out = bands([
            list('A'),
            md(2, 1, '<p>side</p>'),
            md(0, 0, '<p>break</p>'),
            list('B'),
        ]);
        expect(out).toHaveLength(3);
        expect(out[0].cells.map(c => c.kind)).toEqual(['list', 'markdown']);
        expect(out[0].cells[1]).toMatchObject({ kind: 'markdown', html: '<p>side</p>', columns: 2, rows: 1 });
        expect(out[1]).toEqual({ kind: 'markdown', html: '<p>break</p>' });
        expect(out[2].cells.map(c => c.kind)).toEqual(['list']);
    });

    it('carries columns and rows through on grid-card cells', () => {
        const out = bands([md(1, 1, '<p>a</p>'), md(3, 2, '<p>b</p>')]);
        expect(out).toHaveLength(1);
        expect(out[0].cells).toHaveLength(2);
        expect(out[0].cells[0]).toMatchObject({ kind: 'markdown', columns: 1, rows: 1 });
        expect(out[0].cells[1]).toMatchObject({ kind: 'markdown', columns: 3, rows: 2 });
    });

    it('treats a rows-only markdown (server has filled in Columns=1) as a single-column tall card', () => {
        const out = bands([md(1, 2, '<p>tall</p>')]);
        expect(out).toHaveLength(1);
        expect(out[0]).toMatchObject({ kind: 'grid' });
        expect(out[0].cells).toHaveLength(1);
        expect(out[0].cells[0]).toMatchObject({ kind: 'markdown', columns: 1, rows: 2 });
    });

    it('carries float through on grid-card cells', () => {
        const out = bands([md(2, 1, '<p>r</p>', 'right'), md(1, 1, '<p>l</p>')]);
        expect(out).toHaveLength(1);
        expect(out[0].cells[0]).toMatchObject({ kind: 'markdown', columns: 2, float: 'right' });
        expect(out[0].cells[1]).toMatchObject({ kind: 'markdown', columns: 1, float: '' });
    });

    it('treats a markdown with only float=right as a 1×1 grid card, not a band', () => {
        // The server normalises this case (Columns/Rows = 1), so the
        // input that bands() sees here mirrors what the API emits.
        const out = bands([md(1, 1, '<p>x</p>', 'right')]);
        expect(out).toHaveLength(1);
        expect(out[0]).toMatchObject({ kind: 'grid' });
        expect(out[0].cells[0]).toMatchObject({ kind: 'markdown', float: 'right' });
    });

    it('emits two consecutive band markdowns without an empty grid between', () => {
        const out = bands([md(0, 0, '<p>a</p>'), md(0, 0, '<p>b</p>')]);
        expect(out).toEqual([
            { kind: 'markdown', html: '<p>a</p>' },
            { kind: 'markdown', html: '<p>b</p>' },
        ]);
    });
});
