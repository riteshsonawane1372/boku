# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

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
