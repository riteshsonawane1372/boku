# Development

```bash
make build     # bin/boku
make test      # go test ./...
make race      # go test -race ./...
make lint      # gofmt check + go vet
make sample    # render the fictional sample report to ./tmp/sample.pdf
```

## Tests

Unit tests never call Claude Code. `internal/orchestrator/orchestrator_test.go`
uses a scripted fake agent to run the whole pipeline and covers:

- the end-to-end happy path (PDF too when Chrome is installed)
- resume after a failed stage without repeating completed work
- resume after cancellation
- one research workstream failing (with retry) while others succeed
- all research failing
- fact-check follow-up rounds
- fact-check, research and editorial gates blocking publication
- an editorial revision fixing a bad citation
- budget exhaustion
- rendering from artifacts without agents
- untraceable chart data being dropped

Other packages test retries, error classification, environment filtering,
malformed JSON extraction, URL normalisation, tier rules, freshness
classification, de-duplication, citation numbering, table normalisation,
HTML escaping of agent content, chart/diagram determinism, PDF inspection,
gates and configuration.

`go test -short` skips the Chrome-backed PDF tests.

## Trying a real run cheaply

```bash
boku report "What is the current state of WebAssembly outside the browser?" \
  --depth quick --agents 2 --iterations 0 --max-cost 5 --verbose
```

`--verbose` shows every agent start/finish and ingest. Everything is also in
`runs/<id>/logs/boku.log`.

## Debugging a run

- `boku status <run>` — stages, tasks, attempts, cost, errors
- `agents/<task>.raw.json` — the raw Claude Code response
- `evidence/findings.md` — the evidence register in readable form
- `report/gates.json` — why a gate passed or failed
- `report/draft.html` — the blocked draft, when the editorial gate refused it
