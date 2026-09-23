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
                      retry + budget runner
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
only needs to implement `Agent` and be added to `newAgent` in `cmd/boku`.

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
