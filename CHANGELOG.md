# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- PowerPoint export: `--format pptx` publishes a 16:9 deck from any report,
  and `boku slides <topic>` (`--slides`, `--mode slides`) has the report
  written for slides. Charts and diagrams are native shapes, tables are real
  tables, speaker notes carry the full cited text. Written directly as Office
  Open XML: no template, office suite or Chrome needed.
- Expert scoping: `--refine` (and **Refine with an expert** in `boku ui`)
  has a new `scoper` role ask three to five topic-specific questions, then
  rewrite the request into a brief the user confirms, edits or rejects before
  anything is researched. Saved in the run as `scope.json` / `scope.md` and
  passed to the planner.
- Themes: `report.theme` / `--theme` (`auto`, `aurora`, `meridian`, `ember`,
  `slate`) set the cover design, colours and chart palette of the report and
  the deck; `auto` lets the editor choose.
- Dynamic structure: headline metric tiles (`metrics`, traced to evidence
  like charts), a one-sentence `takeaway` per section, and three more chart
  forms (`stacked`, `area`, `donut`).
- Examples: a second research report, decks for both example runs and a
  paper-layout PDF in `docs/examples`, shown in an Examples section on the
  site and in the docs.
- The website is now a Hugo site (`hugo.toml`, sources in `site/`): the
  sitemap (with git `lastmod`), `robots.txt`, canonical URLs, social tags and
  JSON-LD are generated at build time, and the Pages workflow rebuilds on
  every push to `main`. `hugo server` (or `make serve`) previews it.
- Site performance and SEO: page images are served as responsive WebP from
  Hugo's image pipeline, fonts are self-hosted (no third-party requests),
  CSS and JS are minified and fingerprinted, the hero image and font are
  preloaded, and titles and descriptions fit search-result lengths.
- Web interface: `boku ui` serves a local app (default
  `http://127.0.0.1:7878`) to start runs with every setting, watch stages,
  cost and the log live, cancel, resume and re-render, browse a run's plan,
  evidence, fact-check rounds and files, open published reports, and edit
  the defaults in `boku.yaml`. Embedded in the binary; no build step.
- Explainer mode: `boku explain <topic>` (or `--explainer`,
  `--mode explainer`) writes a visual explainer — big picture, key ideas,
  architecture and process diagrams, step-by-step flows, glossary.
- Whitepaper mode: `boku whitepaper <topic>` (or `--whitepaper`,
  `--mode whitepaper`) writes the most detailed format, laid out like a
  conference paper: abstract and keywords, highlights, numbered sections and
  subsections, captioned figures and tables, `[n]` citations and a reference
  list. Deep research; references always included. New `paper` layout
  (`report.layout: paper`) and optional `keywords` in the document schema.
- Codebase explainers: `boku explain <dir> [focus]` has agents read a local
  repository with read-only `Read`/`Glob`/`Grep` tools and cite files by
  path and line range (`research.codebase`).

### Fixed

- The report template rendered the executive summary and key findings twice,
  with stray contents entries between them.
- Paper layout: a long whitepaper could be blocked by the PDF gate for
  "empty pages", because the check assumed a running header the paper layout
  does not have.
- The evidence register's columns no longer squeeze words apart; long URLs
  and identifiers in table cells get soft break points instead.

### Changed

- Report design: Inter (embedded, OFL) replaces the serif body face; new
  covers, chapter openers, figure cards, table and callout styling. Charts
  and diagrams are redrawn with rounded marks, lighter grids and the theme
  palette. The paper layout is unchanged.
- Wide tables can no longer make Chrome shrink the whole PDF to fit.
- The website is redesigned: light only, gradient mesh, scroll parallax.

- A failed fact check no longer blocks publication. Unresolved critical
  issues and a `fail` verdict are shown in a red "Not verified" notice after
  the executive summary and in the methodology appendix; the editor is told
  not to state them as fact. New callout tone `unverified`.

## [0.1.0] - 2026-09-30

### Added

- Report modes: `--short` (3 workstreams, one fact-check round, compact
  brief) and `--quick` (every role on a local Ollama model, no fact-check,
  no Claude Code tokens); `--mode full|short|quick`.
- References file: `<report>.references.json` is always published next to
  the report; the source list and evidence register appear in the document
  only with `--save-ref` (`report.include_references`).
- Request-driven report shape: planner `report_shape`, original request
  passed to synthesis and editorial, optional part titles, optional key
  findings and conclusion, compact layout (`report.layout`).
- Custom model providers via config: `agents.provider: ollama | openai`
  (any OpenAI-compatible endpoint), with token pricing for cost tracking.
- Boku's own web retrieval for providers without web tools (DuckDuckGo or
  SearXNG), context-fitted pages, source re-grounding, private-address
  protection.
- Local formatter role: style-only editorial problems are fixed by a small
  local model under a fact-preservation check.
- Broader research: source-breadth rules for researchers, higher per-depth
  source targets (8 / 20 / 35), 4–8 priority sources per workstream.
- GitHub Pages site (`site/`, `make site`, `.github/workflows/pages.yml`).

- `boku report`, `resume`, `render`, `status`, `doctor`, `init` commands.
- Claude Code agent adapter: restricted mode, web-only tools, filtered
  environment, JSON Schema output, error classification.
- Retry, backoff and run-wide cost budget for agent calls.
- Planner with dynamic role selection; parallel research workstreams
  (primary, market, technical, financial, competitive, case study).
- Evidence store: findings linked to sources, URL de-duplication, claim
  merging, source tiers with domain rules, computed freshness labels.
- Fact checker with verdicts, corrections and follow-up research rounds.
- Synthesis and editorial agents; editorial revision loop.
- Report model with citation resolution, chart traceability checks, table
  normalisation, methodology appendix and evidence register.
- HTML/print-CSS renderer, SVG charts and diagrams, Markdown renderer,
  PDF via headless Chrome over the DevTools pipe.
- Research, fact-check, editorial and PDF quality gates.
- Run directories with manifest, per-stage status and resume.

### Changed

- A missing key-findings list is no longer an editorial-gate error.

[Unreleased]: https://github.com/riteshsonawane1372/boku/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/riteshsonawane1372/boku/releases/tag/v0.1.0
