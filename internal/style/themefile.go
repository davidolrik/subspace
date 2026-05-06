package style

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sblinch/kdl-go"
)

// ResolveTheme returns the palette to use for the named theme along
// with any human-readable warnings describing problems encountered
// while resolving it. ResolveTheme never fails — on any error it
// returns DarkPalette() with a warning explaining why, so a broken
// theme can never block the CLI from running.
//
// Resolution rules:
//   - "" or "dark"  → DarkPalette() (no file lookup)
//   - "light"       → LightPalette()
//   - any other name → <configDir>/themes/<name>.kdl, with each key
//     overriding the corresponding field in DarkPalette(). Missing
//     keys inherit dark; unknown keys produce a warning.
func ResolveTheme(name, configDir string) (Palette, []string) {
	switch name {
	case "", "dark":
		return DarkPalette(), nil
	case "light":
		return LightPalette(), nil
	}

	path := filepath.Join(configDir, "themes", name+".kdl")
	data, err := os.ReadFile(path)
	if err != nil {
		return DarkPalette(), []string{fmt.Sprintf("theme %q: %v", name, err)}
	}

	base := DarkPalette()
	p, warns := applyThemeKDL(data, base)
	for i, w := range warns {
		warns[i] = fmt.Sprintf("theme %q: %s", name, w)
	}
	return p, warns
}

// applyThemeKDL parses a theme file body and overlays any recognized
// keys onto base, returning the resulting palette and warnings.
func applyThemeKDL(data []byte, base Palette) (Palette, []string) {
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return base, []string{fmt.Sprintf("parse: %v", err)}
	}

	p := base
	var warns []string
	for _, node := range doc.Nodes {
		key := node.Name.ValueString()
		if len(node.Arguments) < 1 {
			warns = append(warns, fmt.Sprintf("%s: missing color value", key))
			continue
		}
		val := node.Arguments[0].ValueString()
		if err := setPaletteKey(&p, key, val); err != nil {
			warns = append(warns, err.Error())
		}
	}
	return p, warns
}

// setPaletteKey applies a single key/value pair to p. Returns an error
// (suitable as a warning) if the key is unknown or the value malformed.
func setPaletteKey(p *Palette, key, hex string) error {
	switch key {
	// Foregrounds
	case "heading":
		return setFg(&p.Heading, key, hex)
	case "success":
		return setFg(&p.Success, key, hex)
	case "error":
		return setFg(&p.Error, key, hex)
	case "warning":
		return setFg(&p.Warning, key, hex)
	case "caution":
		return setFg(&p.Caution, key, hex)
	case "info":
		return setFg(&p.Info, key, hex)
	case "notice":
		return setFg(&p.Notice, key, hex)
	case "highlight":
		return setFg(&p.Highlight, key, hex)
	case "strong":
		return setFg(&p.Strong, key, hex)
	case "faint":
		return setFg(&p.Faint, key, hex)
	case "muted":
		return setFg(&p.Muted, key, hex)
	case "body":
		return setFg(&p.Body, key, hex)
	// Backgrounds
	case "bg-success":
		return setBg(&p.BgSuccess, key, hex)
	case "bg-error":
		return setBg(&p.BgError, key, hex)
	case "bg-info":
		return setBg(&p.BgInfo, key, hex)
	case "bg-warning":
		return setBg(&p.BgWarning, key, hex)
	case "bg-debug":
		return setBg(&p.BgDebug, key, hex)
	}
	return fmt.Errorf("unknown key %q", key)
}

func setFg(dst *string, key, hex string) error {
	esc, err := FgFromHex(hex)
	if err != nil {
		return fmt.Errorf("%s: %v", key, err)
	}
	*dst = esc
	return nil
}

func setBg(dst *string, key, hex string) error {
	esc, err := BgFromHex(hex)
	if err != nil {
		return fmt.Errorf("%s: %v", key, err)
	}
	*dst = esc
	return nil
}

// PaletteKeys returns the canonical theme-file key for every Palette
// field, in the order they should appear in an exported theme file.
// Used by themewrite.go and exposed for any future tooling.
func PaletteKeys() []string {
	return []string{
		"heading", "success", "error", "warning", "caution",
		"info", "notice", "highlight",
		"strong", "faint", "muted", "body",
		"bg-success", "bg-error", "bg-info", "bg-warning", "bg-debug",
	}
}
