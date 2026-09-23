# Reports and rendering

## Document model

The editorial agent returns a `report.Document`: title, subtitle, report
type, executive summary paragraphs, key findings, sections of typed blocks,
and conclusion. Block types:

| Type | Fields |
| --- | --- |
| `paragraph` | `text` |
| `subheading` | `text` |
| `bullets` | `items` |
| `callout` | `text`, `title`, `tone` (insight, caution, estimate, note) |
| `table` | `title`, `columns`, `rows`, `finding_ids`, `as_of`, `text` (note) |
| `chart` | `chart` {`kind` column/bar/line, `title`, `units`, `labels`, `series`}, `finding_ids`, `as_of` |
| `diagram` | `diagram` {`kind` architecture/process/timeline, `title`, `nodes`, `edges`}, `finding_ids` |

Text supports `**bold**`, `*italic*` and citation markers `[F001]` /
`[F001, F002]`.

`report.Build` validates the document against the evidence store:

- resolves markers to citation numbers in order of first appearance;
  unknown or rejected findings are errors;
- drops empty sections and numbers the rest;
- normalises ragged table rows and drops empty tables;
- **drops any chart whose values do not appear in its cited findings**
  (allowing thousand/million/billion rescaling), or that lacks a title, units,
  data or date;
- drops diagram edges that reference missing nodes;
- appends a methodology appendix and an evidence register.

## Rendering

`internal/render` produces:

- **HTML** from `report.html.tmpl` and `style.css` — print-first CSS paged
  media: A4/Letter pages, named cover page, running header (title, report
  type, date), footer (run ID, page number), numbered sections, key-findings
  list, callouts, tables with repeating headers, figures with source lines,
  numbered source list with tier labels.
- **SVG charts** (`charts.go`) — column, horizontal bar, line; nice axis
  ticks, value labels, legends for multi-series; a restrained single-accent palette.
- **SVG diagrams** (`diagrams.go`) — layered graph layout (top-down for
  architecture, left-right for process, cycle-safe), and timelines.
- **PDF** (`pdf.go`) — headless Chrome driven over the DevTools pipe:
  load, wait for fonts, `Page.printToPDF` with CSS page size and a document
  outline.
- **Markdown** (`markdown.go`) — charts become data tables so nothing is lost.

`render.InspectPDF` counts pages and text operators per page so the PDF gate
can detect empty pages without a PDF library.

## Adding a renderer

Write `func X(r *report.Report) ([]byte, error)` in `internal/render`, add the
format to `config.Validate` and to `stageRender` in
`internal/orchestrator/publish.go`.

## Adding a chart or diagram type

Add the kind to `prompts/schemas/document.json`, accept it in `report.Build`
(`chart`/`diagram`), draw it in `render.ChartSVG`/`DiagramSVG`, and describe
when to use it in `prompts/editorial.md`. Keep rendering a pure function of
the data.

## Changing the design

Edit `internal/render/style.css` and run `boku render <run-dir>` to re-render
an existing run for free. The sample fixture in `testdata/sample` exercises
every block type.
