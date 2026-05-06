package style

import (
	"strings"
	"testing"
)

func TestParseHexColor(t *testing.T) {
	cases := []struct {
		in            string
		wantR, wantG, wantB int
		wantErr       bool
	}{
		{"#000000", 0, 0, 0, false},
		{"#ffffff", 255, 255, 255, false},
		{"#FFFFFF", 255, 255, 255, false},
		{"#0a7178", 10, 113, 120, false},
		{"#fff", 255, 255, 255, false},
		{"#000", 0, 0, 0, false},
		{"#abc", 0xaa, 0xbb, 0xcc, false},
		{"", 0, 0, 0, true},
		{"#12345", 0, 0, 0, true},
		{"#1234567", 0, 0, 0, true},
		{"#gggggg", 0, 0, 0, true},
		{"123456", 0, 0, 0, true}, // missing leading #
	}
	for _, c := range cases {
		r, g, b, err := ParseHexColor(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseHexColor(%q): expected error, got %d/%d/%d", c.in, r, g, b)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseHexColor(%q): unexpected error %v", c.in, err)
			continue
		}
		if r != c.wantR || g != c.wantG || b != c.wantB {
			t.Errorf("ParseHexColor(%q) = %d/%d/%d, want %d/%d/%d", c.in, r, g, b, c.wantR, c.wantG, c.wantB)
		}
	}
}

// containsAllRGB tracks the bare ANSI escape sequences that ApplyPalette
// must produce for every Palette field.
func TestApplyPaletteMutatesPackageVars(t *testing.T) {
	t.Cleanup(func() { ApplyPalette(DarkPalette()) })

	light := LightPalette()
	ApplyPalette(light)

	// Each Palette field corresponds to a package-level escape var of
	// the same name. The value must match exactly so consumers see the
	// new theme on the next read.
	checks := map[string]string{
		"Heading":   Heading,
		"Success":   Success,
		"Error":     Error,
		"Warning":   Warning,
		"Caution":   Caution,
		"Info":      Info,
		"Notice":    Notice,
		"Highlight": Highlight,
		"Strong":    Strong,
		"Faint":     Faint,
		"Muted":     Muted,
		"Body":      Body,
		"BgSuccess": BgSuccess,
		"BgError":   BgError,
		"BgInfo":    BgInfo,
		"BgWarning": BgWarning,
		"BgDebug":   BgDebug,
	}
	want := map[string]string{
		"Heading":   light.Heading,
		"Success":   light.Success,
		"Error":     light.Error,
		"Warning":   light.Warning,
		"Caution":   light.Caution,
		"Info":      light.Info,
		"Notice":    light.Notice,
		"Highlight": light.Highlight,
		"Strong":    light.Strong,
		"Faint":     light.Faint,
		"Muted":     light.Muted,
		"Body":      light.Body,
		"BgSuccess": light.BgSuccess,
		"BgError":   light.BgError,
		"BgInfo":    light.BgInfo,
		"BgWarning": light.BgWarning,
		"BgDebug":   light.BgDebug,
	}
	for name, got := range checks {
		if got != want[name] {
			t.Errorf("after ApplyPalette(light), %s = %q, want %q", name, got, want[name])
		}
	}
}

func TestDarkPaletteMatchesHistoricalNeons(t *testing.T) {
	// Sanity-check the dark palette preserves the existing neon hues
	// so the visual default doesn't drift after the refactor.
	p := DarkPalette()
	wantContains := map[string]string{
		"Heading": "0;255;234", // cyan
		"Success": "0;255;136", // matrix green
		"Error":   "255;55;95", // alarm red
		"Strong":  "255;255;255",
	}
	for field, sub := range wantContains {
		var got string
		switch field {
		case "Heading":
			got = p.Heading
		case "Success":
			got = p.Success
		case "Error":
			got = p.Error
		case "Strong":
			got = p.Strong
		}
		if !strings.Contains(got, sub) {
			t.Errorf("DarkPalette().%s = %q, want substring %q", field, got, sub)
		}
	}
}

func TestLightPaletteIsForegroundEscapes(t *testing.T) {
	// Every foreground field must be a 24-bit fg escape (38;2;...).
	// Backgrounds use 48;2; — both must be non-empty.
	p := LightPalette()
	fgs := []string{
		p.Heading, p.Success, p.Error, p.Warning, p.Caution,
		p.Info, p.Notice, p.Highlight,
		p.Strong, p.Faint, p.Muted, p.Body,
	}
	for i, esc := range fgs {
		if !strings.HasPrefix(esc, "\033[38;2;") {
			t.Errorf("LightPalette() fg[%d] = %q, missing 38;2; prefix", i, esc)
		}
	}
	bgs := []string{p.BgSuccess, p.BgError, p.BgInfo, p.BgWarning, p.BgDebug}
	for i, esc := range bgs {
		if !strings.HasPrefix(esc, "\033[48;2;") {
			t.Errorf("LightPalette() bg[%d] = %q, missing 48;2; prefix", i, esc)
		}
	}
}
