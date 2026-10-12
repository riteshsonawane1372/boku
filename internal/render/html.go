// Package render turns a report.Report into files. Layout is deterministic:
// agents supply content and data, never markup.
//
//	report.Report ──▶ HTML (print stylesheet, inline SVG charts/diagrams)
//	              ──▶ PDF  (HTML printed by headless Chrome)
//	              ──▶ Markdown
package render

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html"
	"html/template"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riteshsonawane1372/boku/internal/report"
)

//go:embed style.css
var styleCSS string

//go:embed report.html.tmpl
var reportTemplate string

// The report's typeface (Inter, SIL Open Font License; see fonts/OFL.txt),
// subset to Latin, Greek, currency, arrows and maths. It is embedded in the
// HTML so the PDF looks the same on every machine.
//
//go:embed fonts/Inter.woff2
var interFont []byte

var fontFaceCSS = template.CSS(`@font-face { font-family: "Inter"; font-style: normal; font-weight: 100 900; font-display: block; src: url(data:font/woff2;base64,` +
	base64.StdEncoding.EncodeToString(interFont) + `) format("woff2"); }`)

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"inline":       func(s string) template.HTML { return inlineHTML(s, true) },
	"two":          func(n int) string { return fmt.Sprintf("%02d", n) },
	"letter":       func(i int) string { return string(rune('A' + i)) },
	"month":        func(t time.Time) string { return t.Format("January 2006") },
	"day":          func(t time.Time) string { return t.Format("2 January 2006") },
	"date":         func(t *time.Time) string { return fmtDate(t) },
	"sources":      func(b report.Block) template.HTML { return sourceLine(b, true) },
	"chart":        func(b report.Block) template.HTML { return template.HTML(ChartSVG(b.Chart, ThemeFor(""))) },
	"diagram":      func(b report.Block) template.HTML { return template.HTML(DiagramSVG(b.Diagram, ThemeFor(""))) },
	"pagecss":      pageCSS,
	"css":          func() template.CSS { return template.CSS(styleCSS) },
	"fontface":     func() template.CSS { return fontFaceCSS },
	"themecss":     func() template.CSS { return themeCSS(ThemeFor("")) },
	"trend":        trendMark,
	"plain":        report.PlainText,
	"cell":         func(s string) template.HTML { return inlineHTML(breakLong(s), true) },
	"breakLong":    breakLong,
	"add":          func(a, b int) int { return a + b },
	"calloutLabel": calloutLabel,
	"join":         strings.Join,
}).Parse(reportTemplate))

// HTML renders the report as a standalone, print-ready HTML document.
func HTML(r *report.Report) ([]byte, error) {
	t, err := tmpl.Clone()
	if err != nil {
		return nil, err
	}
	// Citations link to the source list only when it is in the document.
	links := r.IncludeReferences
	th := themeOf(r)
	t.Funcs(template.FuncMap{
		"chart":    func(b report.Block) template.HTML { return template.HTML(ChartSVG(b.Chart, th)) },
		"diagram":  func(b report.Block) template.HTML { return template.HTML(DiagramSVG(b.Diagram, th)) },
		"themecss": func() template.CSS { return themeCSS(th) },
		"inline":   func(s string) template.HTML { return inlineHTML(s, links) },
		"cell":     func(s string) template.HTML { return inlineHTML(breakLong(s), links) },
		"sources":  func(b report.Block) template.HTML { return sourceLine(b, links) },
	})
	var buf bytes.Buffer
	if err := t.Execute(&buf, r); err != nil {
		return nil, fmt.Errorf("render html: %w", err)
	}
	return buf.Bytes(), nil
}

// inlineHTML renders built text: escapes content, applies emphasis and links
// citation numbers to the source list.
func inlineHTML(s string, links bool) template.HTML {
	var b strings.Builder
	for _, seg := range report.Inline(s) {
		switch {
		case len(seg.Cites) > 0:
			b.WriteString(`<sup class="cite">`)
			for i, n := range seg.Cites {
				if i > 0 {
					b.WriteString(",")
				}
				if links {
					fmt.Fprintf(&b, `<a href="#src-%d">%d</a>`, n, n)
				} else {
					fmt.Fprintf(&b, "%d", n)
				}
			}
			b.WriteString(`</sup>`)
		case seg.Bold:
			b.WriteString("<strong>" + html.EscapeString(seg.Text) + "</strong>")
		case seg.Italic:
			b.WriteString("<em>" + html.EscapeString(seg.Text) + "</em>")
		default:
			b.WriteString(html.EscapeString(seg.Text))
		}
	}
	return template.HTML(b.String())
}

func sourceLine(b report.Block, links bool) template.HTML {
	var parts []string
	if len(b.SourceNums) > 0 {
		var refs []string
		for _, n := range b.SourceNums {
			if links {
				refs = append(refs, fmt.Sprintf(`<a href="#src-%d">[%d]</a>`, n, n))
			} else {
				refs = append(refs, fmt.Sprintf("[%d]", n))
			}
		}
		parts = append(parts, "Source: "+strings.Join(refs, ", ")+".")
	}
	if b.AsOf != "" {
		parts = append(parts, "Data as of "+html.EscapeString(b.AsOf)+".")
	}
	if b.Chart != nil && b.Chart.Note != "" {
		parts = append(parts, html.EscapeString(b.Chart.Note))
	}
	if b.Type == report.BlockTable && b.Text != "" {
		parts = append(parts, string(inlineHTML(b.Text, links)))
	}
	return template.HTML(strings.Join(parts, " "))
}

// longToken is the length from which a word in a table cell gets soft break
// points.
const longToken = 18

// breakLong adds zero-width spaces to long unbroken tokens (URLs, paths,
// identifiers) so a table cell can wrap them: after a separator inside a long
// token, and otherwise every longToken characters. Ordinary words are left
// alone.
func breakLong(s string) string {
	var b strings.Builder
	for i, field := range strings.Split(s, " ") {
		if i > 0 {
			b.WriteByte(' ')
		}
		if utf8.RuneCountInString(field) < longToken || strings.ContainsAny(field, "\uE000\uE001") {
			b.WriteString(field)
			continue
		}
		run := 0
		rs := []rune(field)
		for j, r := range rs {
			b.WriteRune(r)
			run++
			if j == len(rs)-1 {
				break
			}
			if (strings.ContainsRune("/._-:=,&?", r) && run >= 4 && len(rs)-j > 3) || run >= longToken {
				b.WriteRune('\u200b')
				run = 0
			}
		}
	}
	return b.String()
}

// themeCSS sets the stylesheet's colour variables for a theme.
func themeCSS(t Theme) template.CSS {
	return template.CSS(fmt.Sprintf(`:root { --accent: %s; --accent-2: %s; --accent-3: %s; --accent-soft: %s; --accent-line: %s; --ink: %s; --body: %s; --muted: %s; --faint: %s; --rule: %s; --tint: %s; }`,
		t.Accent, t.Accent2, t.Accent3, mix(t.Accent, "#ffffff", 0.93), mix(t.Accent, "#ffffff", 0.75), t.Ink, t.Body, t.Muted, t.Faint, t.Rule, t.Tint))
}

// trendMark is the arrow shown on a metric tile.
func trendMark(trend string) string {
	switch trend {
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "flat":
		return "→"
	}
	return ""
}

func calloutLabel(b report.Block) string {
	if b.Title != "" {
		return b.Title
	}
	switch b.Tone {
	case "caution":
		return "Caution"
	case "estimate":
		return "Estimate"
	case "note":
		return "Note"
	case "unverified":
		return "Not verified"
	}
	return "Insight"
}

// pageCSS emits the report-specific @page rules: size and running header text.
func pageCSS(m report.Metadata) template.CSS {
	size, height := "A4", "297mm"
	if strings.EqualFold(m.PageSize, "letter") {
		size, height = "letter", "279.4mm"
	}
	title, meta, footer := runningText(m)
	return template.CSS(fmt.Sprintf(`@page { size: %s; @top-left { content: %s; } @top-right { content: %s; } @bottom-left { content: %s; } }
.cover { height: %s; }`, size, cssString(title), cssString(meta), cssString(footer), height))
}

// runningText returns the header (left, right) and footer text on every page.
func runningText(m report.Metadata) (title, meta, footer string) {
	return truncateRunes(m.Title, 70),
		strings.TrimSpace(m.ReportType + " · " + m.Date.Format("January 2006")),
		"Generated with Boku · Run " + m.RunID
}

// MarginGlyphs is the number of glyphs the running header and footer put on
// each page; InspectPDF uses it to tell empty pages from pages with content.
func MarginGlyphs(m report.Metadata) int {
	title, meta, footer := runningText(m)
	return utf8.RuneCountInString(title + meta + footer)
}

// cssString quotes s as a CSS string literal.
func cssString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == '<' || r == '>':
			fmt.Fprintf(&b, "\\%x ", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func fmtDate(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("2 Jan 2006")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func fmtISO(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
