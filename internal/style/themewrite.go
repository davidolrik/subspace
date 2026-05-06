package style

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteThemeFile writes p to path as a self-contained KDL theme file
// with a header and a comment per key. Parent directories are created
// as needed. If path already exists and force is false, returns an
// error without modifying the file.
func WriteThemeFile(path string, p Palette, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("theme file already exists: %s (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	body, err := renderThemeFile(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

// renderThemeFile builds the KDL body for a palette. All keys are
// emitted in PaletteKeys() order with per-key comments.
func renderThemeFile(p Palette) (string, error) {
	hex, err := paletteToHex(p)
	if err != nil {
		return "", err
	}

	comments := map[string]string{
		"heading":    "section headers, labels, banner accent",
		"success":    "OK / healthy / direct",
		"error":      "failures, error markers",
		"warning":    "soft warnings (e.g. fallback notes)",
		"caution":    "stronger warnings (reserved)",
		"info":       "informational / links (reserved)",
		"notice":     "emphasis variation (reserved)",
		"highlight":  "alt accent (reserved)",
		"strong":     "max-contrast emphasis text",
		"faint":      "barely-visible structural",
		"muted":      "secondary text",
		"body":       "quiet readable body text",
		"bg-success": "success badge background",
		"bg-error":   "error badge background",
		"bg-info":    "info badge background",
		"bg-warning": "warning badge background",
		"bg-debug":   "debug badge background",
	}

	var b strings.Builder
	b.WriteString("// subspace theme file\n")
	b.WriteString("// All keys optional — missing keys inherit from the dark theme.\n")
	b.WriteString("// Reload by running any subspace command after editing.\n\n")

	// Group foregrounds and backgrounds visually.
	for _, k := range PaletteKeys() {
		if k == "bg-success" {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%-10s %q  // %s\n", k, hex[k], comments[k])
	}
	return b.String(), nil
}

// paletteToHex inverts the ANSI escapes in p back to "#rrggbb" strings.
// Returns an error only if a field somehow holds a non-conforming
// escape — which shouldn't happen for palettes built via fg/bg.
func paletteToHex(p Palette) (map[string]string, error) {
	pairs := []struct {
		key string
		esc string
	}{
		{"heading", p.Heading},
		{"success", p.Success},
		{"error", p.Error},
		{"warning", p.Warning},
		{"caution", p.Caution},
		{"info", p.Info},
		{"notice", p.Notice},
		{"highlight", p.Highlight},
		{"strong", p.Strong},
		{"faint", p.Faint},
		{"muted", p.Muted},
		{"body", p.Body},
		{"bg-success", p.BgSuccess},
		{"bg-error", p.BgError},
		{"bg-info", p.BgInfo},
		{"bg-warning", p.BgWarning},
		{"bg-debug", p.BgDebug},
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		hex, err := escapeToHex(p.esc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.key, err)
		}
		out[p.key] = hex
	}
	return out, nil
}

// escapeToHex parses a 24-bit ANSI fg or bg escape (e.g.
// "\033[38;2;10;113;120m") and returns "#rrggbb".
func escapeToHex(esc string) (string, error) {
	const fgPrefix = "\033[38;2;"
	const bgPrefix = "\033[48;2;"
	suffix := "m"

	var inner string
	switch {
	case strings.HasPrefix(esc, fgPrefix):
		inner = strings.TrimPrefix(esc, fgPrefix)
	case strings.HasPrefix(esc, bgPrefix):
		inner = strings.TrimPrefix(esc, bgPrefix)
	default:
		return "", fmt.Errorf("not a 24-bit color escape: %q", esc)
	}
	inner = strings.TrimSuffix(inner, suffix)
	parts := strings.Split(inner, ";")
	if len(parts) != 3 {
		return "", fmt.Errorf("expected 3 components, got %q", inner)
	}
	var r, g, b int
	if _, err := fmt.Sscanf(parts[0], "%d", &r); err != nil {
		return "", err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &g); err != nil {
		return "", err
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &b); err != nil {
		return "", err
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b), nil
}
