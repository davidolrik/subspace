// Search popup component for Subspace internal pages.
// Activated by pressing "/" — searches page titles, hostnames, aliases, and links.
document.addEventListener('alpine:init', () => {
    Alpine.data('search', () => ({
        open: false,
        query: '',
        selectedIndex: 0,
        allLinks: [],
        // nav is injected from the parent component via x-data merge
        nav: [],

        // Score a single token against the candidate fields: lower is
        // better. Prefix matches on the primary field (label/name) rank
        // highest, then prefix on secondary fields, then substring matches.
        tokenScore(fields, token) {
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
        },

        // Score a (possibly multi-word) query against fields. Every word
        // must match somewhere in the fields; the total score is the sum
        // of per-word scores. A contiguous phrase match on the full query
        // is preferred and gets a bonus (lower score).
        matchScore(fields, q) {
            const phraseScore = this.tokenScore(fields, q);
            const words = q.split(/\s+/).filter(w => w.length > 0);
            if (words.length <= 1) return phraseScore;
            let total = 0;
            for (const w of words) {
                const s = this.tokenScore(fields, w);
                if (s === Infinity) return Infinity;
                total += s;
            }
            // Prefer the contiguous phrase match when it exists.
            if (phraseScore !== Infinity && phraseScore < total) return phraseScore;
            return total;
        },

        filteredResults() {
            const q = this.query.trim().toLowerCase();

            // Nav/page results (always shown, sorted when query is set)
            let pages;
            if (!q) {
                pages = this.nav.map(item => ({
                    type: 'page',
                    label: item.label,
                    url: item.url,
                    icon: item.icon,
                    meta: this.navMeta(item),
                }));
            } else {
                pages = [];
                for (const item of this.nav) {
                    const score = this.matchScore(
                        [item.label, item.name, item.alias], q
                    );
                    if (score < Infinity) {
                        pages.push({
                            type: 'page',
                            label: item.label,
                            url: item.url,
                            icon: item.icon,
                            meta: this.navMeta(item),
                            score,
                        });
                    }
                }
                pages.sort((a, b) => a.score - b.score);
            }

            // Link results (only shown when query is set)
            let links = [];
            if (q) {
                for (const link of this.allLinks) {
                    const score = this.matchScore(
                        [link.name, link.section, link.page, link.description], q
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

            return [...pages, ...links];
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
            } else if (e.key === 'Enter') {
                e.preventDefault();
                const result = this.filteredResults()[this.selectedIndex];
                if (result) {
                    this.navigate(result.url);
                }
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

        navigate(url) {
            this.open = false;
            this.$nextTick(() => {
                window.location.href = url;
            });
        },

        navMeta(item) {
            if (!item.name) return item.url;
            // Strip protocol to show just the host+path
            return item.url.replace(/^https?:\/\//, '').replace(/\/$/, '');
        },

        iconClass(icon) {
            if (!icon) return 'fa-solid fa-link';
            if (icon.startsWith('si-')) return 'si ' + icon;
            if (icon.startsWith('fa-')) return 'fa-solid ' + icon;
            if (icon.startsWith('mdi-')) return 'mdi ' + icon;
            if (icon.startsWith('nf-')) return 'nf ' + icon;
            return icon;
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
                const resp = await fetch('api/all-links');
                this.allLinks = await resp.json() || [];
            } catch (e) {
                console.error('Failed to load links for search:', e);
            }
        }
    }));
});
