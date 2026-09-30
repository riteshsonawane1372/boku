package render

import (
	"fmt"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// Markdown renders the report as GitHub-flavoured Markdown with numbered
// citations. Charts and diagrams are rendered as data tables so no
// information is lost.
func Markdown(r *report.Report) []byte {
	var b strings.Builder
	m := r.Metadata
	fmt.Fprintf(&b, "# %s\n\n", m.Title)
	if m.Subtitle != "" {
		fmt.Fprintf(&b, "_%s_\n\n", m.Subtitle)
	}
	fmt.Fprintf(&b, "%s · %s · Run `%s`\n\n", m.ReportType, m.Date.Format("January 2006"), m.RunID)

	fmt.Fprintf(&b, "## %s\n\n", r.Labels.Summary)
	for _, p := range r.ExecutiveSummary {
		b.WriteString(mdInline(p.Text) + "\n\n")
	}
	if len(r.KeyFindings) > 0 {
		fmt.Fprintf(&b, "## %s\n\n", r.Labels.KeyFindings)
		for i, k := range r.KeyFindings {
			fmt.Fprintf(&b, "%d. **%s** %s\n", i+1, report.PlainText(k.Headline)+mdCites(k.Headline), mdInline(k.Detail))
		}
		b.WriteString("\n")
	}
	for _, s := range r.Sections {
		fmt.Fprintf(&b, "## %02d — %s\n\n", s.Number, s.Title)
		mdBlocks(&b, s.Blocks)
	}
	if len(r.Conclusion) > 0 {
		fmt.Fprintf(&b, "## %s\n\n", r.Labels.Conclusion)
		for _, p := range r.Conclusion {
			b.WriteString(mdInline(p.Text) + "\n\n")
		}
	}
	if !r.IncludeReferences {
		ref := r.Metadata.ReferencesFile
		if ref == "" {
			ref = "the run's references file"
		}
		fmt.Fprintf(&b, "---\n\n_Numbered citations refer to %d sources listed in %s._\n\n", len(r.Sources), ref)
	} else {
		b.WriteString("## Sources\n\n")
	}
	for _, c := range r.Sources {
		if !r.IncludeReferences {
			break
		}
		fmt.Fprintf(&b, "%d. %s", c.Number, c.Title)
		if c.Publisher != "" {
			fmt.Fprintf(&b, " — %s", c.Publisher)
		}
		if d := fmtDate(c.Published); d != "" {
			fmt.Fprintf(&b, " (%s)", d)
		}
		fmt.Fprintf(&b, ". <%s> · accessed %s · %s\n", c.URL, c.Accessed.Format("2 Jan 2006"), c.TierLabel)
	}
	b.WriteString("\n")
	for i, a := range r.Appendices {
		fmt.Fprintf(&b, "## Appendix %c — %s\n\n", 'A'+i, a.Title)
		mdBlocks(&b, a.Blocks)
	}
	return []byte(b.String())
}

func mdBlocks(b *strings.Builder, blocks []report.Block) {
	for _, bl := range blocks {
		switch bl.Type {
		case report.BlockSubheading:
			fmt.Fprintf(b, "### %s\n\n", bl.Text)
		case report.BlockParagraph:
			b.WriteString(mdInline(bl.Text) + "\n\n")
		case report.BlockBullets:
			for _, it := range bl.Items {
				b.WriteString("- " + mdInline(it) + "\n")
			}
			b.WriteString("\n")
		case report.BlockCallout:
			fmt.Fprintf(b, "> **%s.** %s\n\n", calloutLabel(bl), mdInline(bl.Text))
		case report.BlockTable:
			fmt.Fprintf(b, "**Table %d. %s**\n\n", bl.Number, bl.Title)
			mdTable(b, bl.Columns, bl.Rows)
			mdSourceLine(b, bl)
		case report.BlockChart:
			c := bl.Chart
			fmt.Fprintf(b, "**Figure %d. %s** (%s)\n\n", bl.Number, c.Title, c.Units)
			cols := []string{""}
			for _, s := range c.Series {
				cols = append(cols, nonEmptyStr(s.Name, c.Units))
			}
			var rows [][]string
			for i, l := range c.Labels {
				row := []string{l}
				for _, s := range c.Series {
					row = append(row, fmtNum(s.Values[i]))
				}
				rows = append(rows, row)
			}
			mdTable(b, cols, rows)
			mdSourceLine(b, bl)
		case report.BlockDiagram:
			d := bl.Diagram
			fmt.Fprintf(b, "**Figure %d. %s**\n\n", bl.Number, d.Title)
			labels := map[string]string{}
			for _, n := range d.Nodes {
				labels[n.ID] = n.Label
			}
			if len(d.Edges) == 0 || d.Kind == "timeline" {
				for _, n := range d.Nodes {
					if n.Detail != "" {
						fmt.Fprintf(b, "- **%s** — %s\n", n.Detail, n.Label)
					} else {
						fmt.Fprintf(b, "- %s\n", n.Label)
					}
				}
			} else {
				for _, e := range d.Edges {
					if e.Label != "" {
						fmt.Fprintf(b, "- %s → %s (%s)\n", labels[e.From], labels[e.To], e.Label)
					} else {
						fmt.Fprintf(b, "- %s → %s\n", labels[e.From], labels[e.To])
					}
				}
			}
			b.WriteString("\n")
			mdSourceLine(b, bl)
		}
	}
}

func mdTable(b *strings.Builder, cols []string, rows [][]string) {
	esc := func(s string) string { return strings.ReplaceAll(mdInline(s), "|", `\|`) }
	b.WriteString("|")
	for _, c := range cols {
		b.WriteString(" " + esc(c) + " |")
	}
	b.WriteString("\n|")
	for range cols {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString("|")
		for _, c := range r {
			b.WriteString(" " + esc(c) + " |")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func mdSourceLine(b *strings.Builder, bl report.Block) {
	var parts []string
	if len(bl.SourceNums) > 0 {
		var refs []string
		for _, n := range bl.SourceNums {
			refs = append(refs, fmt.Sprintf("[%d]", n))
		}
		parts = append(parts, "Source: "+strings.Join(refs, ", ")+".")
	}
	if bl.AsOf != "" {
		parts = append(parts, "Data as of "+bl.AsOf+".")
	}
	if len(parts) > 0 {
		b.WriteString("_" + strings.Join(parts, " ") + "_\n\n")
	}
}

func mdInline(s string) string {
	var b strings.Builder
	for _, seg := range report.Inline(s) {
		switch {
		case len(seg.Cites) > 0:
			for _, n := range seg.Cites {
				fmt.Fprintf(&b, "[%d]", n)
			}
		case seg.Bold:
			b.WriteString("**" + seg.Text + "**")
		case seg.Italic:
			b.WriteString("_" + seg.Text + "_")
		default:
			b.WriteString(seg.Text)
		}
	}
	return b.String()
}

func mdCites(s string) string {
	var b strings.Builder
	for _, seg := range report.Inline(s) {
		for _, n := range seg.Cites {
			fmt.Fprintf(&b, "[%d]", n)
		}
	}
	return b.String()
}

func nonEmptyStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
