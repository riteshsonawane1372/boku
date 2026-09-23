# Contributing to Boku

Thanks for helping make Boku better at producing research people can trust.
Before building something, ask the project's question: **does this make Boku
better at producing a high-quality research report?**

## Getting started

```bash
git clone https://github.com/riteshsonawane1372/boku && cd boku
make test
```

Unit tests never call Claude Code or the network. Chrome is only needed for
the PDF tests (`go test -short ./...` skips them).

## What we welcome

- Prompt improvements, backed by before/after runs on the same topic
- New research roles, report block types, chart or diagram kinds, renderers
- Better evidence handling: source tiers, date parsing, de-duplication
- Quality-gate checks that catch real problems
- Bug fixes with a failing test first
- Documentation

Please open an issue before large changes (new providers, new stages,
dependency additions) so we can agree on the shape first.

## Principles

- Simple over clever; small interfaces over frameworks.
- Evidence over generated prose; structured artifacts over raw text.
- Deterministic rendering: agents never produce layout.
- Explicit failures over silent fallbacks.
- No new dependency without a strong reason (today: `yaml.v3`, `x/sync`).

## Pull requests

- `make lint test` passes.
- New behaviour has tests, including failure paths.
- Prompt or schema changes: update `docs/agents.md` if the contract changed,
  and keep schemas and Go types in sync.
- Rendering changes: attach a screenshot or PDF of the sample report
  (`make sample`).
- Add a line to `CHANGELOG.md` under *Unreleased*.
- Keep commits focused; describe *why* in the message.

## Where things live

See [docs/architecture.md](docs/architecture.md). In short: agents in
`internal/agent`, pipeline in `internal/orchestrator`, evidence in
`internal/research`, gates in `internal/validation`, document model in
`internal/report`, rendering in `internal/render`, prompts in `prompts/`.

By contributing you agree that your contributions are licensed under the
Apache License 2.0.
