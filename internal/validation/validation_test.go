package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteshsonawane1372/boku/internal/report"
	"github.com/riteshsonawane1372/boku/internal/research"
)

func store(n int) *research.Store {
	s := research.NewStore()
	out := research.ResearchOutput{}
	for i := 0; i < n; i++ {
		ref := string(rune('a' + i))
		out.Sources = append(out.Sources, research.RawSource{Ref: ref, URL: "https://ex.org/" + ref, Tier: 1})
		out.Findings = append(out.Findings, research.RawFinding{Claim: "Distinct claim number " + ref + " about topic " + ref, SourceRefs: []string{ref}, AsOf: "2026"})
	}
	s.Ingest(out, research.IngestOptions{Now: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), FreshnessDays: 365})
	return s
}

func TestApplyVerdicts(t *testing.T) {
	s := store(4)
	changed, warns := ApplyVerdicts(s, FactCheck{Verdicts: []Verdict{
		{FindingID: "F001", Verdict: "verified"},
		{FindingID: "F002", Verdict: "unsupported", Note: "not in source"},
		{FindingID: "F003", Verdict: "outdated"},
		{FindingID: "F004", Verdict: "weak"},
		{FindingID: "F999", Verdict: "verified"},
	}}, 1)
	if changed != 4 || len(warns) != 1 {
		t.Errorf("changed=%d warns=%v", changed, warns)
	}
	if s.Finding("F002").Usable() {
		t.Error("unsupported finding still usable")
	}
	if s.Finding("F003").Temporal != research.TemporalHistorical {
		t.Error("outdated finding not reclassified")
	}
	if s.Finding("F004").Review.Status != research.ReviewFlagged {
		t.Error("weak finding not flagged")
	}
	// Idempotent.
	if again, _ := ApplyVerdicts(s, FactCheck{Verdicts: []Verdict{{FindingID: "F001", Verdict: "verified"}}}, 1); again != 0 {
		t.Errorf("reapplying changed %d findings", again)
	}
}

func TestResearchGate(t *testing.T) {
	if g := ResearchGate(store(12), 10); !g.Passed {
		t.Errorf("should pass: %v", g.Errors)
	}
	g := ResearchGate(store(8), 10)
	if !g.Passed || len(g.Warnings) == 0 {
		t.Errorf("8/10 sources should pass with a warning: %+v", g)
	}
	if g := ResearchGate(store(3), 10); g.Passed {
		t.Error("3 findings should fail")
	}
}

func TestFactGate(t *testing.T) {
	if g := FactGate(nil, 0); g.Passed {
		t.Error("missing fact check must fail")
	}
	if g := FactGate(&FactCheck{Status: FactFail, Summary: "no evidence"}, 1); g.Passed {
		t.Error("fail status must fail")
	}
	crit := &FactCheck{Status: FactNeedsRevision, Issues: []FactIssue{{Severity: "critical", Description: "fabricated number"}}}
	if g := FactGate(crit, 3); g.Passed {
		t.Error("unresolved critical issue must fail")
	}
	major := &FactCheck{Status: FactNeedsRevision, Issues: []FactIssue{{Severity: "major", Description: "thin"}}}
	if g := FactGate(major, 3); !g.Passed || len(g.Warnings) != 2 {
		t.Errorf("major-only should pass with warnings: %+v", g)
	}
}

func goodReport(t *testing.T) (*report.Report, []report.Issue) {
	s := store(6)
	var exec []string
	for i := 0; i < 3; i++ {
		exec = append(exec, "Platform teams now standardise on shared clusters for training and serving, which changes how capacity is planned and paid for across the organisation "+string(rune('a'+i))+" [F001].")
	}
	doc := report.Document{
		Title:            "T",
		ExecutiveSummary: exec,
		KeyFindings:      []report.KeyFinding{{Headline: "a"}, {Headline: "b"}, {Headline: "c"}},
		Sections: []report.Section{
			{Title: "One", Blocks: []report.Block{{Type: "paragraph", Text: "Alpha [F002]."}, {Type: "paragraph", Text: "Beta [F003]."}}},
			{Title: "Two", Blocks: []report.Block{{Type: "paragraph", Text: "Gamma [F004]."}}},
		},
	}
	return report.Build(doc, s, report.Metadata{}, report.BuildOptions{})
}

func TestEditorialGatePassesGoodReport(t *testing.T) {
	r, issues := goodReport(t)
	g := EditorialGate(r, issues)
	if !g.Passed || NeedsRevision(g) {
		t.Errorf("good report: %+v", g)
	}
}

func TestEditorialGateCatchesProblems(t *testing.T) {
	s := store(6)
	rep := "This sentence is repeated verbatim across the whole report for no reason at all."
	doc := report.Document{
		Title:            "T",
		ExecutiveSummary: []string{"In today's rapidly evolving landscape, we delve into things."},
		Sections: []report.Section{{Title: "One", Blocks: []report.Block{
			{Type: "paragraph", Text: rep}, {Type: "paragraph", Text: rep}, {Type: "paragraph", Text: "No citations here."},
			{Type: "paragraph", Text: "Cites a ghost [F404]."},
		}}},
	}
	r, issues := report.Build(doc, s, report.Metadata{}, report.BuildOptions{})
	g := EditorialGate(r, issues)
	joined := strings.Join(append(g.Errors, g.Warnings...), "\n")
	for _, want := range []string{"unknown finding F404", "executive summary cites no sources", "no key findings",
		"body paragraphs cite evidence", "rapidly evolving", "delve", "repeated sentence"} {
		if !strings.Contains(joined, want) {
			t.Errorf("gate missed %q:\n%s", want, joined)
		}
	}
	if g.Passed || !NeedsRevision(g) {
		t.Error("bad report passed")
	}
}

func TestPDFGate(t *testing.T) {
	r, _ := goodReport(t)
	dir := t.TempDir()
	pdf := filepath.Join(dir, "r.pdf")
	html := `<section id="section-1"></section><section id="section-2"></section><sup class="cite"></sup> counter(page)`
	if g := PDFGate(r, PDFCheck{Path: pdf, Pages: 9, HTML: html}); g.Passed {
		t.Error("missing PDF passed")
	}
	_ = os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644)
	if g := PDFGate(r, PDFCheck{Path: pdf, Pages: 9, HTML: html}); !g.Passed {
		t.Errorf("good PDF failed: %v", g.Errors)
	}
	g := PDFGate(r, PDFCheck{Path: pdf, Pages: 2, EmptyPages: []int{2}, HTML: `<section id="section-1">`})
	joined := strings.Join(g.Errors, "\n")
	for _, want := range []string{"2 pages", "empty pages", `section "Two"`, "no citations", "page numbering"} {
		if !strings.Contains(joined, want) {
			t.Errorf("PDF gate missed %q: %s", want, joined)
		}
	}
}

func TestLimitations(t *testing.T) {
	fc := FactCheck{Issues: []FactIssue{{Severity: "minor", Description: "typo"}, {Severity: "major", Description: "Thin data"}, {Severity: "major", Description: "thin data"}},
		ClaimsToVerify: []string{"X"}}
	got := fc.Limitations()
	if len(got) != 2 || got[0] != "Thin data" || got[1] != "Unverified: X" {
		t.Errorf("limitations = %v", got)
	}
}
