package report

import (
	"strings"
	"testing"
	"time"

	"github.com/riteshsonawane1372/boku/internal/research"
)

func testStore() *research.Store {
	s := research.NewStore()
	s.Ingest(research.ResearchOutput{
		Sources: []research.RawSource{
			{Ref: "a", URL: "https://a.example/1", Title: "A", Tier: 1},
			{Ref: "b", URL: "https://b.example/2", Title: "B", Tier: 2},
			{Ref: "c", URL: "https://c.example/3", Title: "C", Tier: 2},
		},
		Findings: []research.RawFinding{
			{Claim: "Revenue was $4.2 billion in 2025.", Evidence: "Annual report: revenue $4.2 billion; 2024: $3.5 billion.", SourceRefs: []string{"a"}, AsOf: "2025"},
			{Claim: "Market share reached 31% in 2026.", Evidence: "Survey found 31%.", SourceRefs: []string{"b", "c"}, AsOf: "2026-03"},
			{Claim: "Something dubious.", SourceRefs: []string{"c"}},
		},
	}, research.IngestOptions{Now: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), FreshnessDays: 365})
	s.Findings[2].Review.Status = research.ReviewRejected
	return s
}

func build(doc Document) (*Report, []Issue) {
	return Build(doc, testStore(), Metadata{Title: "T"}, BuildOptions{Charts: true, Diagrams: true})
}

func errorsOf(issues []Issue) []string {
	var out []string
	for _, i := range issues {
		if i.Severity == "error" {
			out = append(out, i.Message)
		}
	}
	return out
}

func TestCitationsNumberedByFirstAppearance(t *testing.T) {
	r, issues := build(Document{
		ExecutiveSummary: []string{"Share rose [F002]. Revenue grew [F001]. Again [F002, F001]."},
	})
	if errs := errorsOf(issues); len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(r.Sources) != 3 || r.Sources[0].SourceID != "S002" || r.Sources[2].SourceID != "S001" {
		t.Fatalf("source order = %+v", r.Sources)
	}
	segs := Inline(r.ExecutiveSummary[0].Text)
	var cites [][]int
	for _, s := range segs {
		if len(s.Cites) > 0 {
			cites = append(cites, s.Cites)
		}
	}
	if len(cites) != 3 || len(cites[0]) != 2 || cites[1][0] != 3 || len(cites[2]) != 3 || cites[2][0] != 1 {
		t.Errorf("cites = %v", cites)
	}
	if strings.Contains(PlainText(r.ExecutiveSummary[0].Text), " .") {
		t.Error("space left before full stop where marker was removed")
	}
}

func TestUnknownAndRejectedCitationsAreErrors(t *testing.T) {
	_, issues := build(Document{ExecutiveSummary: []string{"x [F404]", "y [F003]"}})
	errs := errorsOf(issues)
	if len(errs) != 2 || !strings.Contains(errs[0], "unknown finding F404") || !strings.Contains(errs[1], "rejected finding F003") {
		t.Errorf("errors = %v", errs)
	}
}

func TestChartValuesMustBeTraceable(t *testing.T) {
	chart := func(values ...float64) Block {
		return Block{Type: BlockChart, FindingIDs: []string{"F001"}, Chart: &Chart{
			Kind: "column", Title: "Revenue", Units: "USD millions", Labels: []string{"2024", "2025"}[:len(values)],
			Series: []Series{{Name: "Revenue", Values: values}},
		}}
	}
	r, _ := build(Document{Sections: []Section{{Title: "S", Blocks: []Block{chart(3500, 4200)}}}})
	if len(r.Sections) != 1 || r.Sections[0].Blocks[0].Number != 1 {
		t.Fatal("traceable chart (rescaled billions → millions) was dropped")
	}
	if r.Sections[0].Blocks[0].AsOf != "2025" {
		t.Errorf("as_of not derived from finding: %q", r.Sections[0].Blocks[0].AsOf)
	}
	r, issues := build(Document{Sections: []Section{{Title: "S", Blocks: []Block{chart(3500, 5100)}}}})
	if len(r.Sections) != 0 {
		t.Error("chart with invented value was kept")
	}
	if !strings.Contains(issues[0].Message, "does not appear") {
		t.Errorf("issue = %v", issues)
	}
}

func TestChartRequiresMetadata(t *testing.T) {
	for name, c := range map[string]*Chart{
		"no units": {Kind: "bar", Title: "t", Labels: []string{"a"}, Series: []Series{{Values: []float64{31}}}},
		"no title": {Kind: "bar", Units: "%", Labels: []string{"a"}, Series: []Series{{Values: []float64{31}}}},
		"mismatch": {Kind: "bar", Title: "t", Units: "%", Labels: []string{"a", "b"}, Series: []Series{{Values: []float64{31}}}},
	} {
		r, _ := build(Document{Sections: []Section{{Title: "S", Blocks: []Block{{Type: BlockChart, FindingIDs: []string{"F002"}, Chart: c}}}}})
		if len(r.Sections) != 0 {
			t.Errorf("%s: chart kept", name)
		}
	}
}

func TestTablesAreNormalised(t *testing.T) {
	r, issues := build(Document{Sections: []Section{{Title: "S", Blocks: []Block{
		{Type: BlockTable, Title: "T", Text: "Note [F002].", Columns: []string{"a", "b", "c"}, Rows: [][]string{{"1", "2", "3"}, {"only"}, {"1", "2", "3", "4"}}, FindingIDs: []string{"F001"}},
		{Type: BlockTable, Title: "Empty", Columns: []string{"a"}},
	}}}})
	blocks := r.Sections[0].Blocks
	if len(blocks) != 1 {
		t.Fatalf("empty table kept: %d blocks", len(blocks))
	}
	for _, row := range blocks[0].Rows {
		if len(row) != 3 {
			t.Errorf("row not normalised: %v", row)
		}
	}
	if !HasCitation(blocks[0].Text) {
		t.Error("table note citation not resolved")
	}
	if len(blocks[0].SourceNums) != 1 {
		t.Error("table provenance missing")
	}
	if len(errorsOf(issues)) != 0 {
		t.Error(errorsOf(issues))
	}
}

func TestDiagramDropsDanglingEdges(t *testing.T) {
	r, _ := build(Document{Sections: []Section{{Title: "S", Blocks: []Block{{Type: BlockDiagram, Diagram: &Diagram{
		Kind: "architecture", Title: "D", Nodes: []Node{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		Edges: []Edge{{From: "a", To: "b"}, {From: "a", To: "ghost"}, {From: "b", To: "b"}},
	}}}}}})
	if got := len(r.Sections[0].Blocks[0].Diagram.Edges); got != 1 {
		t.Errorf("edges = %d, want 1", got)
	}
}

func TestEmptySectionsOmittedAndNumbered(t *testing.T) {
	r, _ := build(Document{Sections: []Section{
		{Title: "Empty", Blocks: []Block{{Type: BlockParagraph, Text: "  "}}},
		{Title: "One", Blocks: []Block{{Type: BlockParagraph, Text: "x [F001]"}}},
		{Title: "Two", Blocks: []Block{{Type: BlockBullets, Items: []string{"a", ""}}}},
	}})
	if len(r.Sections) != 2 || r.Sections[0].Number != 1 || r.Sections[1].Number != 2 {
		t.Errorf("sections = %+v", r.Sections)
	}
}

func TestGeneratedAppendices(t *testing.T) {
	r, _ := Build(Document{ExecutiveSummary: []string{"x [F001]"}}, testStore(), Metadata{}, BuildOptions{
		Method: &Method{Workstreams: []string{"primary"}, FactCheckRounds: 1, FactCheckStatus: "pass", FreshnessDays: 365, Limitations: []string{"Gap"}},
	})
	if len(r.Appendices) != 2 || r.Appendices[0].Title != "Methodology and evidence quality" || r.Appendices[1].Title != "Evidence register" {
		t.Fatalf("appendices = %+v", r.Appendices)
	}
	reg := r.Appendices[1].Blocks[1]
	if len(reg.Rows) != 1 || reg.Rows[0][0] != "F001" {
		t.Errorf("register should list only cited findings: %v", reg.Rows)
	}
}

func TestInlineEmphasis(t *testing.T) {
	segs := Inline("a **bold** and *it* but 5 * 3 stays")
	var got []string
	for _, s := range segs {
		switch {
		case s.Bold:
			got = append(got, "B:"+s.Text)
		case s.Italic:
			got = append(got, "I:"+s.Text)
		default:
			got = append(got, s.Text)
		}
	}
	want := "a |B:bold| and |I:it| but 5 * 3 stays"
	if strings.Join(got, "|") != want {
		t.Errorf("got %q want %q", strings.Join(got, "|"), want)
	}
}
