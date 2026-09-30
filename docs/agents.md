# Agents

Each role has a prompt in `prompts/<role>.md`. The system prompt is built as:

```text
common.md                  rules every agent follows (evidence, dates, tiers, untrusted web content)
+ researcher.md            method and output contract (research roles only)
+ <role>.md                the role's focus
```

Prompts are Go `text/template`s with `{{.Today}}`, `{{.FreshnessDays}}` and
`{{.Depth}}`. The user message is rendered by `agent.RenderPrompt` from the
task's objective, context, constraints and inputs.

Every agent must return JSON matching a schema in `prompts/schemas/`:

| Schema | Used by | Go type |
| --- | --- | --- |
| `planner.json` | planner | `orchestrator.Plan` |
| `research.json` | primary, market, technical, financial, competitive, case-study | `research.ResearchOutput` |
| `factcheck.json` | fact-checker | `validation.FactCheck` |
| `synthesis.json` | synthesizer | `orchestrator.Synthesis` |
| `document.json` | editorial | `report.Document` |

## Dynamic selection

The planner picks roles per workstream. The orchestrator drops unknown roles,
de-duplicates IDs and keeps at most `max_agents` workstreams. The fact
checker may request follow-up tasks for any research role.

## Adding a research role

1. Write `prompts/<role>.md` (focus only; method and output come from `researcher.md`).
2. Add the role constant to `internal/agent/agent.go` and to `ResearchRoles`.
3. Add it to the `role` enums in `prompts/schemas/planner.json` and
   `factcheck.json`, and to `researchRoles` in `prompts/prompts.go`.
4. Describe it in `prompts/planner.md` so the planner knows when to use it.

## Editing prompts without rebuilding

```bash
cp -r prompts my-prompts
# edit my-prompts/editorial.md
echo 'output: {prompts_directory: ./my-prompts}' > boku.yaml
boku report "…"
```

Files missing from the override directory fall back to the embedded ones.
The manifest records a hash of each role's prompt, so you can tell which
prompt version produced a run.

## Writing good prompts for Boku

- Say what the agent owns and what it must not do.
- Ask for evidence and dates, not conclusions.
- Keep formatting out: the renderer owns layout.
- Test prompt changes on the same topic and compare runs' `evidence/findings.md`
  and `report/gates.json`.

## Formatter (local)

`formatter` is not a research role. It runs on the local Ollama model and
receives only the passages of an editorial draft that have style problems,
with the problems listed. Its rewrites are accepted only when they keep the
same finding citations and numbers (`orchestrator/polish.go`). Prompt:
`prompts/formatter.md`; schema: `prompts/schemas/formatter.json`.

## Report shape

The planner returns `report_shape` (the form the request calls for). The
synthesizer and editor receive the original request in the plan brief and
shape the outline, headings (`summary_title`, `key_findings_title`,
`conclusion_title`) and `layout` to it.
