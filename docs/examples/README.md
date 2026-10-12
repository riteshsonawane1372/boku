# Example output

Everything here was produced by Boku from a real run and is published
unedited. The reports and decks come from the same runs, so a page can be
compared with its slide. The website serves these files from `/examples/`.

| File | What it is | Made with |
| --- | --- | --- |
| [`kubernetes-ai-infrastructure.pdf`](kubernetes-ai-infrastructure.pdf) | Research report, 24 pages, 71 sources, `aurora` theme | `boku report "How Kubernetes is being used for AI infrastructure"` (`--depth quick`) |
| [`kubernetes-ai-infrastructure.pptx`](kubernetes-ai-infrastructure.pptx) | The same run as a PowerPoint deck, 36 slides with speaker notes | `boku render runs/<run> --format pptx` |
| [`llm-inference-open-source-paths.pdf`](llm-inference-open-source-paths.pdf) | Research report, 19 pages, 126 sources | `boku report "Open-source projects to contribute to when moving from SRE into LLM inference"` |
| [`llm-inference-open-source-paths.pptx`](llm-inference-open-source-paths.pptx) | The same run as a deck, 43 slides | `boku report "…" --format pdf,pptx` |
| [`kubernetes-ai-infrastructure-paper-layout.pdf`](kubernetes-ai-infrastructure-paper-layout.pdf) | The Kubernetes report set in the whitepaper (paper) layout, 23 pages, with its reference list | `report.layout: paper` on the same run |
| [`kubernetes-ai-infrastructure-pages-6-7.pdf`](kubernetes-ai-infrastructure-pages-6-7.pdf) | Two pages from an earlier rendering, used in the README walkthrough | — |

The paper-layout file shows the whitepaper *format* (title block, abstract,
numbered sections, bracketed citations, references). It was not written by a
`boku whitepaper` run, which also researches at deep depth and writes in
paper style. To produce one:

```bash
boku whitepaper "Kubernetes as the platform for AI infrastructure"
```

To regenerate these after changing the renderer, re-render the runs (no
agents, no cost) and copy the outputs here:

```bash
boku render runs/<run> --format pdf,pptx --output /tmp/out
```
