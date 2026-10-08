# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

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

### Changed

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
