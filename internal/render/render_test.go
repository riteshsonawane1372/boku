package render

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteshsonawane1372/boku/internal/report"
	"github.com/riteshsonawane1372/boku/internal/research"
)

// sampleReport builds the fictional fixture in testdata/sample.
func sampleReport(t *testing.T) *report.Report {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "sample")
	store, err := research.LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "document.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc report.Document
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	meta := report.Metadata{
		Topic: "How did Northwind Logistics modernise its dispatch platform?", Date: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		RunID: "sample", Generator: "boku test", PageSize: "A4",
	}
	r, issues := report.Build(doc, store, meta, report.BuildOptions{Charts: true, Diagrams: true,
		Method: &report.Method{Workstreams: []string{"primary", "technical", "financial"}, FactCheckRounds: 1, FactCheckStatus: "pass", FreshnessDays: 365}})
	for _, is := range issues {
		if is.Severity == "error" {
			t.Fatalf("fixture has build error: %s", is.Message)
		}
	}
	return r
}

func TestHTMLRendersAllParts(t *testing.T) {
	r := sampleReport(t)
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)
	for _, want := range []string{
		`class="cover"`, "Executive summary", "What the evidence shows", `id="section-1"`,
		`<svg class="chart"`, `<svg class="diagram"`, `<sup class="cite">`, `id="src-1"`,
		"Table 1", "Figure 1", "Evidence register", "counter(page)", "@page { size: A4",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if strings.Contains(html, "[F0") {
		t.Error("unresolved finding marker leaked into HTML")
	}
	if strings.Contains(html, "\uE000") {
		t.Error("citation sentinel leaked into HTML")
	}
	if dir := os.Getenv("BOKU_RENDER_OUT"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "sample.html"), out, 0o644)
		_ = os.WriteFile(filepath.Join(dir, "sample.md"), Markdown(r), 0o644)
	}
}

func TestMarkdownRendersCitations(t *testing.T) {
	md := string(Markdown(sampleReport(t)))
	for _, want := range []string{"## Executive summary", "## Sources", "[1]", "| Measure |", "**Figure 1."} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestHTMLEscapesAgentContent(t *testing.T) {
	store := research.NewStore()
	doc := report.Document{
		Title:            `<script>alert(1)</script>`,
		ExecutiveSummary: []string{`Text with <img src=x onerror=alert(1)> and "quotes"`},
		Sections:         []report.Section{{Title: "S", Blocks: []report.Block{{Type: "paragraph", Text: "<b>x</b>"}}}},
	}
	r, _ := report.Build(doc, store, report.Metadata{RunID: `x"; } body { display:none } @page { x: "`, PageSize: "A4"}, report.BuildOptions{})
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<script>alert") || strings.Contains(s, "<img src=x") || strings.Contains(s, "<b>x</b>") {
		t.Error("agent-supplied markup was not escaped")
	}
	if strings.Contains(s, `display:none } @page`) && !strings.Contains(s, `x\"; }`) {
		t.Error("CSS string not escaped")
	}
}

func TestChartsAreDeterministic(t *testing.T) {
	c := &report.Chart{Kind: "column", Title: "t", Units: "u", Labels: []string{"a", "b"}, Series: []report.Series{{Name: "s", Values: []float64{1, -2.5}}}}
	a, b := ChartSVG(c), ChartSVG(c)
	if a != b || !strings.HasPrefix(a, "<svg") {
		t.Fatal("chart output not deterministic")
	}
	for _, kind := range []string{"bar", "line"} {
		c.Kind = kind
		if s := ChartSVG(c); !strings.Contains(s, "</svg>") {
			t.Errorf("%s chart incomplete", kind)
		}
	}
	flat := &report.Chart{Kind: "line", Labels: []string{"a", "b"}, Series: []report.Series{{Values: []float64{5, 5}}}}
	if s := ChartSVG(flat); strings.Contains(s, "NaN") || strings.Contains(s, "Inf") {
		t.Error("flat series produced NaN coordinates")
	}
}

func TestDiagramHandlesCycles(t *testing.T) {
	d := &report.Diagram{Kind: "architecture", Nodes: []report.Node{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}},
		Edges: []report.Edge{{From: "a", To: "b"}, {From: "b", To: "c"}, {From: "c", To: "a"}}}
	s := DiagramSVG(d)
	if strings.Contains(s, "NaN") || strings.Count(s, "<rect") < 3 {
		t.Errorf("bad diagram: %s", s)
	}
}

func TestPDF(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := FindChrome(""); err != nil {
		t.Skip("no Chrome available:", err)
	}
	dir := t.TempDir()
	if out := os.Getenv("BOKU_RENDER_OUT"); out != "" {
		dir = out
	}
	r := sampleReport(t)
	htmlPath := filepath.Join(dir, "sample.html")
	b, _ := HTML(r)
	if err := os.WriteFile(htmlPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(dir, "sample.pdf")
	if err := (&PDFPrinter{}).Print(context.Background(), htmlPath, pdfPath); err != nil {
		t.Fatal(err)
	}
	info, err := InspectPDF(pdfPath, MarginGlyphs(r.Metadata))
	if err != nil {
		t.Fatal(err)
	}
	if info.Pages < 8 {
		t.Errorf("pages = %d, expected a multi-page report", info.Pages)
	}
	if len(info.EmptyPages) > 0 {
		t.Errorf("empty pages: %v (text ops per page %v)", info.EmptyPages, info.TextOps)
	}
}
