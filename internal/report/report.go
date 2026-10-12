// Package report defines the structured document model that sits between the
// research engine and the renderers.
//
//	editorial agent ──▶ Document (JSON, cites findings as [F001])
//	Build(Document, evidence store) ──▶ Report (validated, citations numbered)
//	renderers (HTML/PDF, Markdown) ──▶ files
//
// Renderers never talk to agents, and agents never produce layout.
package report

import "time"

// Document is what the editorial agent writes. Text fields may cite findings
// with markers like [F001] or [F001, F007], and use **bold** / *italic*.
type Document struct {
	Title            string       `json:"title"`
	Subtitle         string       `json:"subtitle"`
	ReportType       string       `json:"report_type"`
	ExecutiveSummary []string     `json:"executive_summary"`
	KeyFindings      []KeyFinding `json:"key_findings"`
	Sections         []Section    `json:"sections"`
	Conclusion       []string     `json:"conclusion"`
	Appendices       []Section    `json:"appendices,omitempty"`

	// The editor shapes the report to the request: it may rename the fixed
	// parts ("Verdict", "Recommendation", "TL;DR"), leave out key findings or
	// the conclusion, and ask for a compact layout (a brief or one-pager).
	SummaryTitle     string `json:"summary_title,omitempty"`
	KeyFindingsTitle string `json:"key_findings_title,omitempty"`
	ConclusionTitle  string `json:"conclusion_title,omitempty"`
	Layout           string `json:"layout,omitempty"` // full | compact
	// Theme is the visual template the editor picks for the subject (see
	// Themes); configuration may override it.
	Theme string `json:"theme,omitempty"`
	// Metrics are the report's headline numbers, shown as tiles under the
	// executive summary and on the deck's "at a glance" slide.
	Metrics []Metric `json:"metrics,omitempty"`
	// Keywords are index terms shown under a paper's abstract.
	Keywords []string `json:"keywords,omitempty"`
}

type KeyFinding struct {
	Headline string `json:"headline"`
	Detail   string `json:"detail"`
}

type Section struct {
	Number int    `json:"number,omitempty"`
	Title  string `json:"title"`
	// Takeaway is the section's point in one sentence: the standfirst under
	// the heading and the headline of the section's slide.
	Takeaway string  `json:"takeaway,omitempty"`
	Blocks   []Block `json:"blocks"`
}

// Metric is one headline number: a value with its unit, what it measures and
// optional context. Build drops metrics whose numbers cannot be traced to the
// cited findings.
type Metric struct {
	Value  string `json:"value"`  // "USD 4.2bn", "62%", "3.4×"
	Label  string `json:"label"`  // what the number measures
	Detail string `json:"detail"` // period, comparison or caveat
	// Trend colours the tile: up, down or flat; empty for no direction.
	Trend      string   `json:"trend,omitempty"`
	FindingIDs []string `json:"finding_ids,omitempty"`
}

// Block types.
const (
	BlockSubheading = "subheading"
	BlockParagraph  = "paragraph"
	BlockBullets    = "bullets"
	BlockTable      = "table"
	BlockChart      = "chart"
	BlockDiagram    = "diagram"
	BlockCallout    = "callout"
	BlockMetrics    = "metrics"
)

// Block is one element of a section. Only the fields relevant to Type are set.
type Block struct {
	Type    string     `json:"type"`
	Text    string     `json:"text,omitempty"`
	Title   string     `json:"title,omitempty"`
	Items   []string   `json:"items,omitempty"`
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
	// Tone styles callouts: insight, caution, estimate, note, unverified.
	Tone       string   `json:"tone,omitempty"`
	FindingIDs []string `json:"finding_ids,omitempty"`
	AsOf       string   `json:"as_of,omitempty"`
	Chart      *Chart   `json:"chart,omitempty"`
	Diagram    *Diagram `json:"diagram,omitempty"`
	Metrics    []Metric `json:"metrics,omitempty"`

	// Filled by Build.
	Number     int   `json:"number,omitempty"`      // figure / table number
	SourceNums []int `json:"source_nums,omitempty"` // provenance for tables, charts, diagrams
}

// Chart is data for a deterministic chart. Every value must come from the
// referenced findings; Build drops charts whose numbers cannot be traced.
type Chart struct {
	Kind   string   `json:"kind"` // see ChartKinds
	Title  string   `json:"title"`
	Units  string   `json:"units"`
	Labels []string `json:"labels"`
	Series []Series `json:"series"`
	Note   string   `json:"note,omitempty"`
}

// ChartKinds are the chart forms the renderers draw.
//
//	column   vertical bars, series side by side
//	stacked  vertical bars, series stacked into a total
//	bar      horizontal bars, for rankings and long labels
//	line     series over ordered periods
//	area     line with the area under it filled
//	donut    shares of a whole (one series)
var ChartKinds = []string{"column", "stacked", "bar", "line", "area", "donut"}

type Series struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

// Diagram is an intermediate representation rendered deterministically.
type Diagram struct {
	Kind  string `json:"kind"` // architecture, process, timeline
	Title string `json:"title"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group,omitempty"`
	// Detail is a short second line (e.g. a date on timelines).
	Detail string `json:"detail,omitempty"`
}

type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// Report is the validated, render-ready model.
type Report struct {
	Metadata         Metadata     `json:"metadata"`
	ExecutiveSummary []Block      `json:"executive_summary"`
	KeyFindings      []KeyFinding `json:"key_findings"`
	Sections         []Section    `json:"sections"`
	Conclusion       []Block      `json:"conclusion"`
	Sources          []Citation   `json:"sources"`
	Appendices       []Section    `json:"appendices"`
	// Evidence lists every cited finding with its status and sources. It is
	// always written to the references file; the PDF shows it only when
	// IncludeReferences is set.
	Evidence []EvidenceEntry `json:"evidence,omitempty"`
	// Labels are the headings of the fixed parts.
	Labels Labels `json:"labels"`
	// Layout is full (cover and contents), compact (title block, no cover)
	// or paper (academic paper: title block, abstract, numbered sections).
	Layout string `json:"layout"`
	// Theme is the resolved visual template (see Themes).
	Theme string `json:"theme"`
	// Metrics are the headline numbers that survived validation.
	Metrics []Metric `json:"metrics,omitempty"`
	// Keywords are index terms shown under a paper's abstract.
	Keywords []string `json:"keywords,omitempty"`
	// IncludeReferences puts the numbered source list into the document.
	IncludeReferences bool `json:"include_references"`
	// Unverified lists points the fact checker could not verify. They are
	// shown in a highlighted notice after the executive summary.
	Unverified []string `json:"unverified,omitempty"`
	// Warnings are non-fatal problems found while building (dropped charts, …).
	Warnings []string `json:"warnings,omitempty"`
}

// Layouts.
const (
	LayoutFull    = "full"
	LayoutCompact = "compact"
	LayoutPaper   = "paper"
)

// Themes are the visual templates shared by the PDF/HTML report and the
// slide deck. Each has its own palette and cover design.
//
//	aurora    indigo and violet gradient mesh; the default
//	meridian  navy and teal with a full-bleed dark cover
//	ember     warm orange band cover
//	slate     monochrome, typographic
var Themes = []string{"aurora", "meridian", "ember", "slate"}

// IsTheme reports whether name is a known theme.
func IsTheme(name string) bool {
	for _, t := range Themes {
		if t == name {
			return true
		}
	}
	return false
}

// Labels are headings for the parts every report has.
type Labels struct {
	Summary     string `json:"summary"`
	KeyFindings string `json:"key_findings"`
	Conclusion  string `json:"conclusion"`
}

// EvidenceEntry is one cited finding in the references file.
type EvidenceEntry struct {
	ID      string `json:"id"`
	Claim   string `json:"claim"`
	Status  string `json:"status"`
	AsOf    string `json:"as_of,omitempty"`
	Sources []int  `json:"sources"`
}

type Metadata struct {
	Title      string    `json:"title"`
	Subtitle   string    `json:"subtitle"`
	ReportType string    `json:"report_type"`
	Topic      string    `json:"topic"`
	Date       time.Time `json:"date"`
	RunID      string    `json:"run_id"`
	Author     string    `json:"author,omitempty"`
	Generator  string    `json:"generator"`
	PageSize   string    `json:"page_size"`
	// ReferencesFile is the name of the companion references JSON file.
	ReferencesFile string `json:"references_file,omitempty"`
}

// Citation is an entry in the numbered source list.
type Citation struct {
	Number    int        `json:"number"`
	SourceID  string     `json:"source_id"`
	Title     string     `json:"title"`
	Publisher string     `json:"publisher,omitempty"`
	URL       string     `json:"url"`
	Published *time.Time `json:"published,omitempty"`
	Accessed  time.Time  `json:"accessed"`
	Tier      int        `json:"tier"`
	TierLabel string     `json:"tier_label"`
}
