package style

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// 24-bit ANSI escape builders
var (
	Reset = "\033[0m"
	Bold  = "\033[1m"
	Dim   = "\033[2m"
	Ital  = "\033[3m"
)

// fg returns a 24-bit foreground color escape.
func fg(r, g, b int) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

// bg returns a 24-bit background color escape.
func bg(r, g, b int) string {
	return fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
}

// Theme-driven color escapes. Initialized from DarkPalette() in init();
// reassigned by ApplyPalette when a different theme loads. Consumers
// read these directly (e.g. style.Heading) and pick up the active
// palette on the next read.
var (
	// Foregrounds — primary roles
	Heading   string
	Success   string
	Error     string
	Warning   string
	Caution   string
	Info      string
	Notice    string
	Highlight string

	// Foregrounds — text weight / contrast
	Strong string
	Faint  string
	Muted  string
	Body   string

	// Badge backgrounds
	BgSuccess string
	BgError   string
	BgInfo    string
	BgWarning string
	BgDebug   string
)

func init() {
	ApplyPalette(DarkPalette())
}

// Colorize wraps text in a foreground color and reset.
func Colorize(c, text string) string {
	if !enabled {
		return text
	}
	return c + text + Reset
}

// BoldC wraps text in bold + color and reset.
func BoldC(c, text string) string {
	if !enabled {
		return text
	}
	return Bold + c + text + Reset
}

// Badge renders text as a colored pill/badge with background.
func Badge(fgc, bgc, text string) string {
	if !enabled {
		return text
	}
	return bgc + fgc + Bold + " " + text + " " + Reset
}

// Separator returns a dim horizontal rule.
func Separator(width int) string {
	if !enabled {
		return ""
	}
	s := Faint
	for i := 0; i < width; i++ {
		s += "─"
	}
	return s + Reset
}

// SectionHeader renders a section title.
func SectionHeader(text string) string {
	if !enabled {
		return text
	}
	return BoldC(Heading, text)
}

// KeyVal formats a key=value pair with cyber styling.
func KeyVal(key, val string) string {
	return Colorize(Muted, key) + Colorize(Faint, "=") + val
}

// upstreamColors — vivid neons avoiding red and green (reserved for status indicators).
var upstreamColors = []string{
	fg(0, 200, 255),   // sky blue
	fg(255, 170, 50),  // warm amber
	fg(180, 130, 255), // soft violet
	fg(0, 220, 210),   // teal
	fg(255, 140, 200), // bubblegum pink
	fg(120, 180, 255), // periwinkle
	fg(255, 200, 80),  // gold
	fg(200, 100, 255), // electric purple
	fg(100, 230, 230), // aqua
	fg(255, 160, 120), // peach
	fg(150, 150, 255), // cornflower
	fg(220, 180, 255), // lilac
	fg(80, 210, 255),  // cerulean
	fg(255, 210, 150), // apricot
	fg(170, 200, 255), // ice blue
	fg(230, 150, 230), // orchid
	fg(50, 180, 220),  // steel blue
	fg(240, 190, 100), // honey
	fg(160, 110, 230), // iris
	fg(60, 200, 180),  // seafoam
	fg(240, 120, 180), // flamingo
	fg(100, 160, 240), // bluebell
	fg(230, 180, 60),  // marigold
	fg(180, 80, 230),  // grape
	fg(80, 210, 200),  // mint
	fg(230, 140, 100), // terracotta
	fg(130, 130, 230), // slate blue
	fg(200, 160, 240), // wisteria
	fg(60, 190, 240),  // azure
	fg(240, 195, 130), // sandstone
	fg(150, 180, 240), // powder blue
	fg(210, 130, 210), // plum
}

// UpstreamColor returns a deterministic color for an upstream name.
func UpstreamColor(name string) string {
	if name == "direct" {
		return Success
	}
	var h uint32
	for _, c := range name {
		h = h*31 + uint32(c)
	}
	return upstreamColors[h%uint32(len(upstreamColors))]
}

// enabled tracks whether color output is supported for direct terminal output.
var enabled = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// ForceColorize wraps text in color regardless of the terminal detection flag.
func ForceColorize(c, text string) string {
	return c + text + Reset
}

// ForceBoldC wraps text in bold + color regardless of the terminal detection flag.
func ForceBoldC(c, text string) string {
	return Bold + c + text + Reset
}

// ForceBadge renders a colored badge regardless of the terminal detection flag.
func ForceBadge(fgc, bgc, text string) string {
	return bgc + fgc + Bold + " " + text + " " + Reset
}
