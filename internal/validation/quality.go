package validation

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/report"
	"github.com/riteshsonawane1372/boku/internal/research"
)

// Gate is the outcome of one quality gate. A gate with errors blocks publication.
type Gate struct {
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func (g *Gate) errorf(format string, args ...any) {
	g.Errors = append(g.Errors, fmt.Sprintf(format, args...))
}
func (g *Gate) warnf(format string, args ...any) {
	g.Warnings = append(g.Warnings, fmt.Sprintf(format, args...))
}

func (g *Gate) done() Gate {
	g.Passed = len(g.Errors) == 0
	return *g
}

// ResearchGate checks the evidence base: enough sources, claims sourced, and
// time-sensitive data dated.
func ResearchGate(store *research.Store, minSources int) Gate {
	g := &Gate{Name: "research"}
	st := store.Stats()
	switch {
	case st.Usable == 0:
		g.errorf("no usable findings")
	case st.Usable < 5:
		g.errorf("only %d usable findings; too few to support a report", st.Usable)
	}
	switch {
	case st.CitedSources < (minSources+1)/2:
		g.errorf("%d distinct sources support usable findings; at least %d required (half of the %d target)", st.CitedSources, (minSources+1)/2, minSources)
	case st.CitedSources < minSources:
		g.warnf("%d distinct sources, below the target of %d", st.CitedSources, minSources)
	}
	if st.Usable > 0 {
		strong := st.SourcesByTier[1] + st.SourcesByTier[2]
		if st.CitedSources > 0 && float64(strong)/float64(st.CitedSources) < 0.3 {
			g.warnf("only %d of %d sources are tier 1–2", strong, st.CitedSources)
		}
		if unk := st.ByTemporal[research.TemporalUnknown]; float64(unk)/float64(st.Usable) > 0.5 {
			g.warnf("%d of %d usable findings have no reliable date", unk, st.Usable)
		}
	}
	if st.Rejected > 0 {
		g.warnf("%d findings rejected (unsourced or failed fact-check)", st.Rejected)
	}
	return g.done()
}

// FactGate checks the final fact-check round.
func FactGate(last *FactCheck, rounds int) Gate {
	g := &Gate{Name: "fact-check"}
	if last == nil {
		g.errorf("fact check did not run")
		return g.done()
	}
	if last.Status == FactFail {
		g.errorf("fact checker judged the evidence insufficient: %s", last.Summary)
	}
	for _, is := range last.CriticalIssues() {
		g.errorf("unresolved critical issue after %d round(s): %s", rounds, is.Description)
	}
	if last.Status == FactNeedsRevision && len(last.CriticalIssues()) == 0 {
		g.warnf("fact checker still requested revisions after %d round(s); remaining issues are listed as limitations", rounds)
	}
	for _, is := range last.Issues {
		if is.Severity == "major" {
			g.warnf("major issue: %s", is.Description)
		}
	}
	return g.done()
}

// Phrases that mark generic machine-written prose.
var clichés = []string{
	"in today's rapidly evolving", "rapidly evolving landscape", "this report explores", "this report will",
	"delve", "navigate the complexities", "game-changer", "game changer", "unlock the power", "unlock the potential",
	"tapestry", "in the realm of", "it is important to note", "it's important to note", "a testament to",
	"ever-evolving", "in conclusion", "paradigm shift", "cutting-edge", "seamlessly", "revolutionize",
	"revolutionise", "harness the power", "landscape of", "stands as a",
}

var sentenceSplit = regexp.MustCompile(`[.!?]+\s+`)

var markdownRe = regexp.MustCompile(`(?m)^#{1,6} |` + "```")

// StyleProblems lists the style problems in one passage of report text:
// generic phrasing and Markdown syntax.
func StyleProblems(text string) []string {
	var out []string
	lower := strings.ToLower(text)
	for _, c := range clichés {
		if strings.Contains(lower, c) {
			out = append(out, fmt.Sprintf("generic phrasing: %q", c))
		}
	}
	if markdownRe.MatchString(text) {
		out = append(out, "Markdown syntax")
	}
	return out
}

// Sentences splits text into lower-cased sentences of 8 or more words, the
// unit the repetition check compares.
func Sentences(text string) []string {
	var out []string
	for _, s := range sentenceSplit.Split(text, -1) {
		s = strings.ToLower(strings.TrimSpace(s))
		if len(strings.Fields(s)) >= 8 {
			out = append(out, s)
		}
	}
	return out
}

// EditorialGate checks the built report: structure, citations, repetition,
// and style. Build errors (unknown citations etc.) are errors here.
func EditorialGate(r *report.Report, buildIssues []report.Issue) Gate {
	g := &Gate{Name: "editorial"}
	for _, is := range buildIssues {
		if is.Severity == "error" {
			g.errorf("%s", is.Message)
		}
	}
	if len(r.ExecutiveSummary) == 0 {
		g.errorf("executive summary is missing")
	}
	if strings.TrimSpace(r.Metadata.Title) == "" {
		g.errorf("report has no title")
	}
	// Key findings are optional: the editor may shape a report without them
	// (a brief, a verdict, a how-to), but a long report should have them.
	switch n := len(r.KeyFindings); {
	case n == 0 && len(r.Sections) >= 5:
		g.warnf("no key findings in a %d-section report", len(r.Sections))
	case n > 0 && n < 3 && r.Layout != report.LayoutCompact:
		g.warnf("only %d key findings", n)
	}
	switch n := len(r.Sections); {
	case n == 0:
		g.errorf("report has no body sections")
	case n < 2:
		g.warnf("report has only one body section")
	}

	var execText []string
	execCited := false
	for _, b := range r.ExecutiveSummary {
		execText = append(execText, report.PlainText(b.Text))
		execCited = execCited || report.HasCitation(b.Text)
	}
	if len(r.ExecutiveSummary) > 0 && !execCited {
		g.errorf("executive summary cites no sources")
	}
	minWords := 120
	if r.Layout == report.LayoutCompact {
		minWords = 50
	}
	if w := len(strings.Fields(strings.Join(execText, " "))); w > 0 && w < minWords {
		g.warnf("executive summary is short (%d words)", w)
	} else if w > 1400 {
		g.warnf("executive summary is long (%d words)", w)
	}

	var paras, cited int
	var all []string
	for _, s := range r.Sections {
		for _, b := range s.Blocks {
			if b.Type == report.BlockParagraph {
				paras++
				if report.HasCitation(b.Text) {
					cited++
				}
			}
			for _, t := range append([]string{b.Text}, b.Items...) {
				if t != "" {
					all = append(all, report.PlainText(t))
				}
			}
		}
	}
	if paras > 0 {
		cov := float64(cited) / float64(paras)
		switch {
		case cov < 0.25:
			g.errorf("only %d of %d body paragraphs cite evidence", cited, paras)
		case cov < 0.5:
			g.warnf("only %d of %d body paragraphs cite evidence", cited, paras)
		}
	}

	full := strings.Join(append(execText, all...), "\n")
	lower := strings.ToLower(full)
	for _, c := range clichés {
		if strings.Contains(lower, c) {
			g.warnf("generic phrasing: %q", c)
		}
	}
	if strings.Contains(full, "```") || regexp.MustCompile(`(?m)^#{1,6} `).MatchString(full) {
		g.warnf("Markdown syntax in text")
	}
	seen := map[string]int{}
	for _, s := range sentenceSplit.Split(full, -1) {
		s = strings.ToLower(strings.TrimSpace(s))
		if len(strings.Fields(s)) < 8 {
			continue
		}
		seen[s]++
	}
	rep := 0
	for s, n := range seen {
		if n > 1 {
			rep++
			if rep <= 3 {
				g.warnf("repeated sentence: %q", truncate(s, 90))
			}
		}
	}
	if rep > 5 {
		g.errorf("%d sentences are repeated verbatim", rep)
	}
	return g.done()
}

// NeedsRevision reports whether an editorial gate result is worth another
// editorial pass: any error, or several style problems.
func NeedsRevision(g Gate) bool {
	return len(g.Errors) > 0 || len(g.Warnings) >= 3
}

// PDFCheck is what the PDF gate needs from the renderer.
type PDFCheck struct {
	Path       string
	Pages      int
	EmptyPages []int
	HTML       string
}

// PDFGate checks the rendered output.
func PDFGate(r *report.Report, c PDFCheck) Gate {
	g := &Gate{Name: "pdf"}
	info, err := os.Stat(c.Path)
	if err != nil || info.Size() == 0 {
		g.errorf("PDF was not generated")
		return g.done()
	}
	if r.Layout == report.LayoutCompact {
		if c.Pages < 1 {
			g.errorf("PDF has no pages")
		}
	} else if c.Pages < 3 {
		g.errorf("PDF has %d pages; expected cover, contents and body", c.Pages)
	}
	if len(c.EmptyPages) > 0 {
		g.errorf("PDF has empty pages: %v", c.EmptyPages)
	}
	for _, s := range r.Sections {
		if !strings.Contains(c.HTML, fmt.Sprintf(`id="section-%d"`, s.Number)) {
			g.errorf("section %q was not rendered", s.Title)
		}
	}
	if !strings.Contains(c.HTML, `class="cite"`) {
		g.errorf("no citations rendered")
	}
	if len(r.Sources) == 0 {
		g.errorf("source list is empty")
	}
	if !strings.Contains(c.HTML, "counter(page)") {
		g.errorf("page numbering missing from stylesheet")
	}
	for _, s := range append(r.Sections, r.Appendices...) {
		for _, b := range s.Blocks {
			if b.Type == report.BlockTable {
				for _, row := range b.Rows {
					if len(row) != len(b.Columns) {
						g.errorf("table %q has a row with %d cells for %d columns", b.Title, len(row), len(b.Columns))
					}
				}
			}
		}
	}
	return g.done()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
