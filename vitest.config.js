import { defineConfig } from 'vitest/config';

// Tests live in /test rather than alongside the frontend assets so the
// Go //go:embed pattern in pages/embed.go does not pull them into the
// production binary.
export default defineConfig({
    test: {
        include: ['test/**/*.test.js'],
    },
});
