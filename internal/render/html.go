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

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"inline":       func(s string) template.HTML { return inlineHTML(s, true) },
	"two":          func(n int) string { return fmt.Sprintf("%02d", n) },
	"letter":       func(i int) string { return string(rune('A' + i)) },
	"month":        func(t time.Time) string { return t.Format("January 2006") },
	"day":          func(t time.Time) string { return t.Format("2 January 2006") },
	"date":         func(t *time.Time) string { return fmtDate(t) },
	"sources":      func(b report.Block) template.HTML { return sourceLine(b, true) },
	"chart":        func(b report.Block) template.HTML { return template.HTML(ChartSVG(b.Chart)) },
	"diagram":      func(b report.Block) template.HTML { return template.HTML(DiagramSVG(b.Diagram)) },
	"pagecss":      pageCSS,
	"css":          func() template.CSS { return template.CSS(styleCSS) },
	"add":          func(a, b int) int { return a + b },
	"calloutLabel": calloutLabel,
}).Parse(reportTemplate))

// HTML renders the report as a standalone, print-ready HTML document.
func HTML(r *report.Report) ([]byte, error) {
	t, err := tmpl.Clone()
	if err != nil {
		return nil, err
	}
	// Citations link to the source list only when it is in the document.
	links := r.IncludeReferences
	t.Funcs(template.FuncMap{
		"inline":  func(s string) template.HTML { return inlineHTML(s, links) },
		"sources": func(b report.Block) template.HTML { return sourceLine(b, links) },
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
