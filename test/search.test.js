import { describe, it, expect } from 'vitest';
import {
    matchEngine,
    matchEnginePrefixes,
    buildEngineURL,
    buildResults,
    autocompleteFor,
    commonPrefix,
    tabCompleteFrom,
    pollConfigVersion,
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

    it('does not emit a fallback when local matches exist', () => {
        const rows = buildResults({
            query: 'GitHub',
            nav,
            allLinks,
            engines,
            defaultEngine: 'google',
        });
        const fallback = rows.find(r => r.type === 'engine');
        expect(fallback).toBeUndefined();
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

    it('preserves keyword expansion when the top result is an exact engine match', () => {
        const results = [
            { type: 'engine', engine: { name: 'metacpan' }, query: 'ojo' },
            { type: 'link', label: 'ojo' },
        ];
        expect(tabCompleteFrom(results, 'cpan ojo')).toBe('metacpan ojo');
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
