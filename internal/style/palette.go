package style

import (
	"fmt"
	"strconv"
	"strings"
)

// Palette is the full set of role-named colors that drive subspace's
// terminal output. Foreground fields hold a complete 24-bit fg escape
// (e.g. "\033[38;2;0;255;234m"); background fields hold a bg escape.
// ApplyPalette copies these onto the package-level color vars so all
// consumers (style.Heading, style.Success, …) immediately reflect the
// active theme.
type Palette struct {
	// Foregrounds — primary roles
	Heading string // section headers, labels, banner accent
	Success string // OK / healthy / "direct"
	Error   string // failures, error markers
	Warning string // soft warnings (e.g. fallback notes)
	Caution string // reserved for stronger warnings
	Info    string // reserved for informational / links
	Notice  string // reserved for emphasis variation
	Highlight string // reserved for alt accent

	// Foregrounds — text weight / contrast
	Strong string // bold high-contrast emphasis (semantic "max-contrast text")
	Faint  string // barely-visible structural ("=", separators)
	Muted  string // secondary text (KeyVal keys, log keys)
	Body   string // quiet readable body text

	// Badge backgrounds
	BgSuccess string
	BgError   string
	BgInfo    string
	BgWarning string
	BgDebug   string
}

// DarkPalette is the original neon-on-black palette. It is the default
// applied at package init and the safe fallback when a theme can't be
// resolved.
func DarkPalette() Palette {
	return Palette{
		Heading:   fg(0, 255, 234),   // Tron-style cyan
		Success:   fg(0, 255, 136),   // Matrix phosphor green
		Error:     fg(255, 55, 95),   // Alarm red
		Warning:   fg(255, 183, 0),   // Warm amber / CRT phosphor
		Caution:   fg(255, 234, 0),   // Warning yellow
		Info:      fg(30, 144, 255),  // Electric blue
		Notice:    fg(255, 41, 117),  // Cyberpunk hot pink
		Highlight: fg(187, 134, 252), // Synthwave purple
		Strong:    fg(255, 255, 255), // Bright white
		Faint:     fg(88, 91, 112),   // Barely visible
		Muted:     fg(140, 143, 161), // Muted text
		Body:      fg(180, 190, 210), // Readable but quiet
		BgSuccess: bg(0, 60, 55),     // Dark teal
		BgError:   bg(70, 10, 20),    // Dark crimson
		BgInfo:    bg(0, 60, 55),     // Mirrors BgSuccess (historical alias)
		BgWarning: bg(70, 50, 0),     // Dark amber
		BgDebug:   bg(35, 35, 45),    // Dark slate
	}
}

// LightPalette is tuned for white / off-white terminal backgrounds.
// All foregrounds clear ~4.5:1 contrast on #ffffff while preserving
// each role's hue identity so muscle memory survives the swap.
func LightPalette() Palette {
	return Palette{
		Heading:   fg(10, 113, 120),  // #0a7178 deep teal
		Success:   fg(32, 112, 64),   // #207040 forest
		Error:     fg(194, 64, 80),   // #c24050 brick
		Warning:   fg(181, 137, 0),   // #b58900 dark mustard
		Caution:   fg(154, 133, 0),   // #9a8500 dark gold
		Info:      fg(30, 96, 192),   // #1e60c0 link blue
		Notice:    fg(199, 40, 120),  // #c72878 magenta-rose
		Highlight: fg(108, 92, 231),  // #6c5ce7 deep violet
		Strong:    fg(0, 0, 0),       // semantic "max-contrast text" → black on light
		Faint:     fg(170, 178, 190), // #aab2be very faint structural
		Muted:     fg(120, 128, 135), // #788087 muted text
		Body:      fg(74, 82, 96),    // #4a5260 quiet body text
		BgSuccess: bg(223, 245, 232), // #dff5e8 pale mint
		BgError:   bg(253, 224, 230), // #fde0e6 pale rose
		BgInfo:    bg(223, 245, 232), // mirrors BgSuccess
		BgWarning: bg(255, 244, 214), // #fff4d6 pale buttery
		BgDebug:   bg(232, 234, 242), // #e8eaf2 pale slate
	}
}

// ApplyPalette overrides the package-level color escape vars. Safe to
// call once at startup before any output. Not safe for concurrent use
// after subcommands begin printing.
func ApplyPalette(p Palette) {
	Heading = p.Heading
	Success = p.Success
	Error = p.Error
	Warning = p.Warning
	Caution = p.Caution
	Info = p.Info
	Notice = p.Notice
	Highlight = p.Highlight
	Strong = p.Strong
	Faint = p.Faint
	Muted = p.Muted
	Body = p.Body
	BgSuccess = p.BgSuccess
	BgError = p.BgError
	BgInfo = p.BgInfo
	BgWarning = p.BgWarning
	BgDebug = p.BgDebug
}

// ParseHexColor parses #rrggbb or #rgb (case-insensitive). Returns the
// component values as 0–255 ints. The leading # is required.
func ParseHexColor(s string) (r, g, b int, err error) {
	if len(s) == 0 || s[0] != '#' {
		return 0, 0, 0, fmt.Errorf("hex color must start with #")
	}
	hex := strings.ToLower(s[1:])
	switch len(hex) {
	case 3:
		// Expand #rgb → #rrggbb
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	case 6:
		// already canonical
	default:
		return 0, 0, 0, fmt.Errorf("hex color must be #rgb or #rrggbb, got %q", s)
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex digits in %q: %w", s, err)
	}
	r = int((n >> 16) & 0xff)
	g = int((n >> 8) & 0xff)
	b = int(n & 0xff)
	return r, g, b, nil
}

// FgFromHex builds a 24-bit foreground escape from a hex color string.
// Returns an error for malformed input — the caller decides whether to
// warn-and-skip or hard-fail.
func FgFromHex(s string) (string, error) {
	r, g, b, err := ParseHexColor(s)
	if err != nil {
		return "", err
	}
	return fg(r, g, b), nil
}

// BgFromHex builds a 24-bit background escape from a hex color string.
func BgFromHex(s string) (string, error) {
	r, g, b, err := ParseHexColor(s)
	if err != nil {
		return "", err
	}
	return bg(r, g, b), nil
}
