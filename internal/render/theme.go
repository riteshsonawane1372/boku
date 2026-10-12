package render

import (
	"fmt"
	"strconv"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// Theme is a visual template: the colours shared by the report stylesheet,
// the charts and diagrams, and the slide deck. Colours are "#rrggbb".
type Theme struct {
	Name string
	// Accent carries headings, rules and the first chart series; Accent2 and
	// Accent3 are used for gradients and secondary marks.
	Accent, Accent2, Accent3 string
	Ink, Body, Muted, Faint  string
	Rule, Tint               string
	// Palette is the categorical series order for charts and diagram groups.
	Palette []string
}

var themes = map[string]Theme{
	"aurora": {
		Name: "aurora", Accent: "#4f46e5", Accent2: "#7c3aed", Accent3: "#06b6d4",
		Palette: []string{"#4f46e5", "#06b6d4", "#f59e0b", "#ec4899", "#10b981", "#94a3b8"},
	},
	"meridian": {
		Name: "meridian", Accent: "#0b3b5c", Accent2: "#0e9f9a", Accent3: "#f2a541",
		Palette: []string{"#0b3b5c", "#0e9f9a", "#f2a541", "#6c8ead", "#d1495b", "#a3b1c2"},
	},
	"ember": {
		Name: "ember", Accent: "#c2410c", Accent2: "#e11d48", Accent3: "#f59e0b",
		Palette: []string{"#c2410c", "#1f2937", "#f59e0b", "#0f766e", "#9f1239", "#a8a29e"},
	},
	"slate": {
		Name: "slate", Accent: "#111827", Accent2: "#2563eb", Accent3: "#64748b",
		Palette: []string{"#111827", "#2563eb", "#9ca3af", "#f97316", "#14b8a6", "#d1d5db"},
	},
}

// ThemeFor returns the named theme, or the default for an unknown name.
func ThemeFor(name string) Theme {
	t, ok := themes[name]
	if !ok {
		t = themes[report.Themes[0]]
	}
	t.Ink, t.Body, t.Muted, t.Faint = "#0f172a", "#334155", "#64748b", "#94a3b8"
	t.Rule, t.Tint = "#e2e8f0", "#f8fafc"
	return t
}

// themeOf is the theme a report renders with. Papers are typeset in black.
func themeOf(r *report.Report) Theme {
	if r.Layout == report.LayoutPaper {
		return ThemeFor("slate")
	}
	return ThemeFor(r.Theme)
}

func (t Theme) series(i int) string { return t.Palette[i%len(t.Palette)] }

// mix blends colour a towards b: t=0 is a, t=1 is b.
func mix(a, b string, t float64) string {
	ar, ag, ab := rgb(a)
	br, bg, bb := rgb(b)
	l := func(x, y int) int { return int(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", l(ar, br), l(ag, bg), l(ab, bb))
}

func rgb(hex string) (int, int, int) {
	if len(hex) != 7 {
		return 0, 0, 0
	}
	v, _ := strconv.ParseUint(hex[1:], 16, 32)
	return int(v >> 16), int(v >> 8 & 0xff), int(v & 0xff)
}
