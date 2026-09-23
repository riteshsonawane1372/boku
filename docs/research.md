# Research model

## Findings and sources

A **finding** is one claim with its evidence:

```json
{
  "id": "F012",
  "claim": "CNCF's 2025 survey found 52% of respondents run AI inference on Kubernetes.",
  "evidence": "Survey of 2,100 organisations, question 14 …",
  "source_ids": ["S007"],
  "as_of": "2025-11",
  "kind": "reported",
  "temporal": "current",
  "confidence": 0.85,
  "workstream": "market",
  "review": {"status": "accepted", "verdict": "verified"}
}
```

A **source** is a document:

```json
{"id": "S007", "url": "https://…", "title": "…", "publisher": "CNCF",
 "published_at": "2025-11-18T00:00:00Z", "accessed_at": "2026-09-23T…", "tier": 1,
 "found_by": ["research-market"]}
```

The store (`internal/research`) de-duplicates sources by normalised URL
(scheme, `www.`, tracking parameters, fragments and trailing slashes ignored)
and merges near-identical claims, combining their sources. Findings keep stable
IDs; the editor cites `[F012]`, and the renderer turns that into numbered
references to S007.

## Kinds and temporal status

Agents label each finding's **kind**: `reported`, `estimated`, `derived`
(calculated by the agent; calculation in `evidence`) or `inference`.

Boku computes **temporal status** deterministically:

| Status | Rule |
| --- | --- |
| estimated / derived / inference | from kind |
| unknown | no parseable `as_of` and no dated source |
| current | the period described ends within `freshness_days` of the run date |
| historical | older |

`as_of` accepts `2026-03-14`, `2026-03`, `2026`, `Q1 2026`, `2026-Q1`,
`FY2025`; periods are judged by their end. If a finding has no date, the
newest publication date of its sources is used. The fact checker can mark a
finding `outdated`, which forces `historical`.

## Source tiers

| Tier | Examples |
| --- | --- |
| 1 | government, regulators, filings, official documentation, standards bodies, peer-reviewed papers |
| 2 | major financial and news publications, established research organisations |
| 3 | specialist publications, industry blogs, analyst commentary |
| 4 | forums, social media, anonymous sources, aggregators |

Agents propose a tier; `research.ResolveTier` applies domain rules the agent
cannot override (e.g. `reddit.com` is always tier 4, self-publishing platforms
are at most tier 3, `sec.gov` and `.gov` are tier 1).

## Rejection rules

- A non-inference finding with no resolvable source is rejected at ingest.
- The fact checker's `unsupported` and `contradicted` verdicts reject;
  `weak` flags (usable, never as a headline); `outdated` relabels.
- Rejected findings are excluded from synthesis and editorial input, and
  citing one is an editorial-gate error.

## Quality gates

| Gate | Blocks when | Warns when |
| --- | --- | --- |
| research | < 5 usable findings; distinct sources < half of `min_sources` | sources < `min_sources`; < 30% tier 1–2; > 50% undated |
| fact-check | final status `fail`; unresolved critical issue | still `needs_revision`; major issues |
| editorial | citation of unknown/rejected finding; no executive summary, key findings or sections; uncited executive summary; < 25% of body paragraphs cited; > 5 repeated sentences | clichés, repetition, thin structure, Markdown in text |
| pdf | no PDF; < 3 pages; empty pages; a section missing; no citations or sources; page numbering missing; ragged table | — |

`min_sources` defaults to 5 / 10 / 20 for quick / standard / deep.
