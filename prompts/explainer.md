## This run produces an explainer

The output is a **visual explainer**, not a research report. Its reader wants
to understand how something works: the core ideas, the parts and how they fit
together, and what happens step by step. Clarity beats coverage, and a good
diagram beats a paragraph. Evidence rules are unchanged: every claim still
keeps its source.
{{if .Codebase}}
**The subject is a local codebase.** Your working directory is the root of
the repository. Read it with the Read, Glob and Grep tools; nothing can be
changed. The code, its configuration and its own documentation are the
primary sources, and they outrank anything on the web.

- A source is a file. Set `url` to its repository-relative path, with a line
  range where one helps (`internal/agent/claude.go#L48-L75`), `title` to what
  the file is ("Claude Code CLI adapter"), `publisher` to the repository
  name, `source_type` to "source code", "configuration", "documentation" or
  "tests", and `tier` to 1. Leave `published_at` empty.
- A finding describes what the code does, set `as_of` to {{.Today}}, and
  quote the identifiers, signatures or short snippets it rests on in
  `evidence`. Name real files, types and functions; never invent one.
- What the code does is `reported`; why it is built that way is `inference`
  unless a comment, doc or commit message says so.
- Web search is for the external libraries, protocols and services the code
  relies on, and only when the repository does not explain them.
{{end}}
{{if eq .Role "planner"}}
### Planning an explainer

- `report_type`: "Explainer"{{if .Codebase}} or "Codebase Guide"{{end}}.
- `report_shape`: an explainer — the big picture first in plain language, then
  the core concepts, then how the parts fit together, then step-by-step
  walkthroughs of the main flows, then where to go next.
- `research_questions`: what it is and what problem it solves; its core
  concepts and vocabulary; its components and how they connect; the main
  flows, step by step; the important design decisions and trade-offs; common
  pitfalls. Quantitative questions only where numbers aid understanding.
{{- if .Codebase}}
- Workstreams use the `technical` and `primary` roles only. Split the
  repository by subsystem or by flow (one workstream reads the entry points
  and the request path, another the storage layer, …), not by source type.
  Name the directories and files each workstream should start from in
  `priority_sources`.
{{- end}}
- `deliverables`: mostly diagrams — one architecture diagram of the whole,
  process diagrams for the two or three main flows, a timeline only if
  history matters — plus a glossary table{{if .Codebase}} and a "where
  things live" table mapping concepts to directories and files{{end}}.
{{end}}
{{if or (eq .Role "primary") (eq .Role "market") (eq .Role "technical") (eq .Role "financial") (eq .Role "competitive") (eq .Role "case-study")}}
### Researching for an explainer

Collect what a reader needs to understand the subject, not market data:
definitions of each core concept; components, their responsibilities and
their connections (direction and what flows along each); the steps of each
main flow in order; the design decisions and their stated reasons; limits
and common mistakes. Record structures precisely enough to draw them.
{{end}}
{{if eq .Role "fact-checker"}}
### Checking an explainer

Check that each described component, connection and step exists and is in
the right order{{if .Codebase}} by opening the cited files{{end}}, and that
reasons given for design decisions are sourced or labelled as inference.
Missing market or financial data is not a gap for an explainer; do not raise
it.
{{end}}
{{if eq .Role "synthesizer"}}
### Synthesising an explainer

The `thesis` is a plain-language answer to "what is it and how does it
work", in a few sentences a newcomer can follow. Order the outline so each
section builds on the previous one: big picture → core concepts → how the
parts fit → step-by-step flows → decisions and trade-offs → where to go next.
Plan a diagram for most sections.
{{end}}
{{if eq .Role "editorial"}}
### Writing an explainer

Write for a smart newcomer. Define each term the first time it appears, then
use it consistently. Use short paragraphs, concrete examples and, where it
helps, one brief analogy. Explain *what* before *how*, and *how* before
*why*.

- `report_type`: "Explainer"{{if .Codebase}} or "Codebase Guide"{{end}}.
  `summary_title`: "The big picture". `key_findings_title`: "Key ideas" —
  3–6 one-line ideas the reader should leave with. `conclusion_title`:
  "Where to go next".
- The executive summary says in plain words what the subject is, what
  problem it solves and how it works at the highest level.
- Lead with visuals. Open the first body section with an `architecture`
  diagram of the whole. Give each main flow its own section with a `process`
  diagram followed by numbered-style steps (a `bullets` block whose items
  start with the step's name in bold). Aim for a diagram in most sections;
  split a big system into several small diagrams rather than one crowded
  one.
- Include a glossary `table` (Term, Meaning){{if .Codebase}} and a "Where
  things live" `table` (Concept, Location, Notes) whose cells name real
  directories and files{{end}}.
- Use `insight` callouts for the one idea per section that makes the rest
  click, and `caution` callouts for pitfalls.
{{- if .Codebase}}
- Refer to code by its real names in `*italics*` (the *Runner* type, the
  *stageFactCheck* function) and say which file holds it. Do not paste code
  blocks; describe what the code does.
{{- end}}
- Use `layout: full` unless the request asks for something short.
{{end}}
