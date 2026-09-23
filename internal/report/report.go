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
}

type KeyFinding struct {
	Headline string `json:"headline"`
	Detail   string `json:"detail"`
}

type Section struct {
	Number int     `json:"number,omitempty"`
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
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
)

// Block is one element of a section. Only the fields relevant to Type are set.
type Block struct {
	Type    string     `json:"type"`
	Text    string     `json:"text,omitempty"`
	Title   string     `json:"title,omitempty"`
	Items   []string   `json:"items,omitempty"`
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
	// Tone styles callouts: insight, caution, estimate, note.
	Tone       string   `json:"tone,omitempty"`
	FindingIDs []string `json:"finding_ids,omitempty"`
	AsOf       string   `json:"as_of,omitempty"`
	Chart      *Chart   `json:"chart,omitempty"`
	Diagram    *Diagram `json:"diagram,omitempty"`

	// Filled by Build.
	Number     int   `json:"number,omitempty"`      // figure / table number
	SourceNums []int `json:"source_nums,omitempty"` // provenance for tables, charts, diagrams
}

// Chart is data for a deterministic chart. Every value must come from the
// referenced findings; Build drops charts whose numbers cannot be traced.
type Chart struct {
	Kind   string   `json:"kind"` // bar (horizontal), column, line
	Title  string   `json:"title"`
	Units  string   `json:"units"`
	Labels []string `json:"labels"`
	Series []Series `json:"series"`
	Note   string   `json:"note,omitempty"`
}

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
	// Warnings are non-fatal problems found while building (dropped charts, …).
	Warnings []string `json:"warnings,omitempty"`
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
