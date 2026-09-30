# Architecture

Boku is a single Go process that orchestrates agent processes. There is no
server, queue or database: the run directory is the state.

## Pipeline

| # | Stage | Input | Output artifact | Agent |
| - | ----- | ----- | --------------- | ----- |
| 1 | plan | topic, config | `plan.json` | planner |
| 2 | research | plan | `research/<workstream>.json` → `evidence/` | one per workstream, in parallel |
| 3 | factcheck | evidence corpus | `factcheck/round-N.json`, `research/followup-rN-XX.json` | fact-checker ⟲ researchers |
|   | *research gate, fact gate* | evidence, last round | `report/gates.json` | — |
| 4 | synthesis | plan, validated corpus | `synthesis/synthesis.json` | synthesizer |
| 5 | editorial | plan, synthesis, corpus | `report/document.json`, `document-revN.json` | editorial ⟲ revision |
| 6 | report | final draft, evidence | `report/report.json` | — (*editorial gate*) |
| 7 | render | report model | `report/report.html`, `.md`, `output/*.pdf` | — (*PDF gate*) |

A gate with errors stops the run with status `blocked` and exit code 3.
Nothing is copied to the output directory unless every gate passed.

## Packages

```text
cmd/boku              CLI: flags, config layering, commands
internal/config       YAML config, defaults, validation
internal/agent        Agent interface, Task/Result contract, Claude Code adapter,
                      ChatModel (Ollama / OpenAI-compatible), Web retrieval,
                      Router (per-role providers), retry + budget runner
internal/run          run directory, manifest, atomic artifact I/O
internal/research     evidence model: sources, findings, tiers, freshness, store
internal/validation   fact-check application, quality gates
internal/report       document model, citation resolution, chart/table/diagram validation
internal/render       HTML + print CSS, SVG charts and diagrams, Markdown, PDF, PDF inspection
internal/orchestrator the pipeline
internal/logx         CLI logger
prompts/              agent prompts and JSON Schemas (embedded)
```

Dependencies point one way: `orchestrator` → everything else;
`render` → `report` → `research`. The research engine never imports the
renderer, and the renderer never sees an agent.

## The agent boundary

```go
type Agent interface {
    Run(ctx context.Context, task Task) (Result, error)
}
```

A `Task` carries the role's system prompt, the objective, context,
constraints, input artifacts, a JSON Schema, the allowed tools, model,
timeout, budget and a scratch directory. A `Result` carries the structured
JSON output, raw provider response and usage (cost, turns, attempts).

`agent.ClaudeCode` implements it by running:

```text
claude -p --output-format json --restricted --tools WebSearch,WebFetch
       --allowedTools WebSearch,WebFetch --strict-mcp-config
       --no-session-persistence --disable-slash-commands --permission-mode dontAsk
       --system-prompt <role prompt> --json-schema <schema>
       [--model m] [--max-budget-usd b]          (user prompt on stdin)
```

Errors are classified (`unavailable`, `timeout`, `malformed_output`,
`runtime`, `budget`) so `agent.Runner` knows what to retry. A new provider
only needs to implement `Agent` and be added to `newAgents` in `cmd/boku`.

`agent.ChatModel` implements it for Ollama (`/api/chat`) and any
OpenAI-compatible endpoint (`/chat/completions`). These models have no web
tools, so when a task grants web tools and lists `SearchQueries`, Boku runs
the searches itself (`agent.Web`: DuckDuckGo or SearXNG), fetches pages
through a client that refuses non-public addresses, fits the page text to
the model's context window, and passes the pages as numbered sources. The
output's `sources` array is then re-grounded against the fetched pages, so a
model cannot cite a URL Boku did not fetch.

`agent.Router` sends each role to its runtime: `local.roles` (all roles in
`--quick`) go to the local Ollama model, everything else to
`agents.provider`. The orchestrator also holds an optional `Formatter`
(the local model) that rewrites style-flagged passages of the editorial
draft; a rewrite is kept only if its citation markers and numbers are
unchanged.

## Report modes and references

`report.mode` (`--short`, `--quick`) adjusts depth, workstreams, fact-check
rounds, layout and routing through `config.ApplyMode` before explicit flags
are applied. Every published report gets `<slug>.references.json`
(`render.ReferencesJSON`); the source list and evidence register render into
the document only when `report.include_references` (`--save-ref`) is set.

## Concurrency

Research workstreams and follow-ups run through an `errgroup` limited to
`agents.max_parallel`. One failed workstream does not cancel the others; if
every workstream fails the run fails. The evidence store is only mutated by
the orchestrator goroutine after tasks complete, so it needs no locking. The
manifest is updated under a mutex and written atomically.

## Resume

Every step checks for its artifact before calling an agent, and ingestion
into the evidence store is recorded in `evidence/ingested.json`, so replaying
the pipeline over a partially complete run is idempotent:

```text
planner        ✓   plan.json exists → loaded
primary        ✓   research/primary.json exists → skipped
technical      ✗   missing → runs now
fact-check     -
```

`boku resume` reuses the run's recorded configuration (operational flags such
as `--max-cost` and `--parallel` may change). `boku render` re-runs only the
deterministic stages.

## Cost control

- `agents.max_agents` caps workstreams per plan and follow-ups per round.
- `agents.max_parallel` caps concurrency.
- `research.max_iterations` caps fact-check → follow-up rounds and editorial revisions.
- `agents.max_retries` caps retries per call.
- `agents.max_cost_usd` is a run-wide budget: the runner refuses to start a
  task once it is spent and passes the remainder to Claude Code as
  `--max-budget-usd`. Spend from earlier attempts of a resumed run counts.
- Follow-up researchers receive the list of known claims so they do not
  repeat work; parallel researchers see each other's scopes.
