package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/report"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/internal/validation"
)

// Synthesis is the synthesis agent's output (prompts/schemas/synthesis.json).
type Synthesis struct {
	Thesis     string `json:"thesis"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
	ReportType string `json:"report_type"`
	Insights   []struct {
		Statement  string   `json:"statement"`
		Basis      string   `json:"basis"`
		FindingIDs []string `json:"finding_ids"`
		Confidence string   `json:"confidence"`
	} `json:"insights"`
	Contradictions []struct {
		Description string   `json:"description"`
		Resolution  string   `json:"resolution"`
		FindingIDs  []string `json:"finding_ids"`
	} `json:"contradictions"`
	Gaps    []string `json:"gaps"`
	Outline []struct {
		Title      string   `json:"title"`
		Purpose    string   `json:"purpose"`
		FindingIDs []string `json:"finding_ids"`
		Visuals    []string `json:"visuals"`
	} `json:"outline"`
}

func (o *Orchestrator) planBrief(st *state) string {
	p := st.plan
	return mustJSON(map[string]any{
		"original_request": st.run.Manifest().Topic,
		"objective":        p.Objective, "title": p.Title, "subtitle": p.Subtitle, "report_type": p.ReportType,
		"audience": p.Audience, "report_shape": p.ReportShape, "research_questions": p.ResearchQuestions,
		"deliverables": p.Deliverables, "proposed_outline": p.ReportOutline,
		"report_mode": o.Config.Report.Mode,
	})
}

func (o *Orchestrator) stageSynthesis(ctx context.Context, st *state) (string, error) {
	const rel = "synthesis/synthesis.json"
	if !st.run.Exists(rel) {
		inputs := []agentArtifact{{"research plan", o.planBrief(st)}, {"validated research corpus", st.store.CorpusJSON(true)}}
		if st.lastFC != nil && len(st.lastFC.Issues) > 0 {
			inputs = append(inputs, agentArtifact{"open fact-check issues", mustJSON(st.lastFC.Issues)})
		}
		_, err := o.runTask(ctx, st, taskSpec{
			ID: "synthesis", Role: agent.RoleSynthesizer, Stage: run.StageSynthesis, Schema: "synthesis",
			Objective: "Synthesize the validated research into the argument and structure of the report.",
			Context:   fmt.Sprintf("Findings flagged as weakly supported must not carry a headline. Findings are labelled current/historical relative to %s.", st.asOf.Format("2 January 2006")),
			Inputs:    toArtifacts(inputs), Artifact: rel,
		})
		if err != nil {
			return "", fmt.Errorf("synthesis: %w", err)
		}
	}
	var syn Synthesis
	if _, err := st.run.ReadJSON(rel, &syn); err != nil {
		return "", err
	}
	_ = st.run.WriteFile("synthesis/synthesis.md", []byte(synthesisMarkdown(syn)))
	o.Log.Info("synthesis completed", "insights", len(syn.Insights), "sections", len(syn.Outline), "gaps", len(syn.Gaps))
	return fmt.Sprintf("%d insights, %d planned sections", len(syn.Insights), len(syn.Outline)), nil
}

func (o *Orchestrator) loadSynthesis(st *state) (Synthesis, error) {
	var syn Synthesis
	_, err := st.run.ReadJSON("synthesis/synthesis.json", &syn)
	return syn, err
}

// stageEditorial writes the report and revises it until the editorial gate
// passes or the revision budget is used.
func (o *Orchestrator) stageEditorial(ctx context.Context, st *state) (string, error) {
	syn, err := o.loadSynthesis(st)
	if err != nil {
		return "", err
	}
	synJSON, _ := json.MarshalIndent(syn, "", " ")
	corpus := st.store.CorpusJSON(true)
	maxRevisions := max(1, o.Config.Research.MaxIterations)

	rel := "report/document.json"
	if !st.run.Exists(rel) {
		_, err := o.runTask(ctx, st, taskSpec{
			ID: "editorial", Role: agent.RoleEditorial, Stage: run.StageEditorial, Schema: "document",
			Objective: "Write the complete report.",
			Inputs:    toArtifacts([]agentArtifact{{"research plan", o.planBrief(st)}, {"synthesis", string(synJSON)}, {"research corpus", corpus}}),
			Constraints: []string{
				"Shape the report to the original request in the research plan: its wording decides the form, length and headings.",
			},
			Artifact: rel,
		})
		if err != nil {
			return "", fmt.Errorf("editorial: %w", err)
		}
	}
	evaluate := func(rel string) (report.Document, validation.Gate, []report.Issue, error) {
		doc, err := o.readDraft(st, rel)
		if err != nil {
			return doc, validation.Gate{}, nil, err
		}
		r, issues := report.Build(doc, st.store, o.metadata(st, doc), o.buildOptions(st, syn))
		return doc, validation.EditorialGate(r, issues), issues, nil
	}
	for rev := 1; ; rev++ {
		doc, gate, issues, err := evaluate(rel)
		if err != nil {
			return "", err
		}
		// Style-only problems are fixed by the local formatter first: it
		// rewrites just the flagged passages and costs no provider tokens.
		if validation.NeedsRevision(gate) && len(gate.Errors) == 0 && st.fmtRun != nil && !strings.HasSuffix(rel, polishedSuffix) {
			polished := strings.TrimSuffix(rel, ".json") + polishedSuffix
			if st.run.Exists(polished) || o.polish(ctx, st, doc, polished) == nil {
				rel = polished
				if doc, gate, issues, err = evaluate(rel); err != nil {
					return "", err
				}
			}
		}
		if !validation.NeedsRevision(gate) {
			st.docPath = rel
			break
		}
		if rev > maxRevisions {
			st.docPath = rel
			o.Log.Warn("editorial revision budget used", "revisions", maxRevisions)
			break
		}
		next := fmt.Sprintf("report/document-rev%d.json", rev)
		if !st.run.Exists(next) {
			problems := append(append([]string{}, gate.Errors...), gate.Warnings...)
			for _, is := range issues {
				if is.Severity == "warning" && strings.Contains(is.Message, "dropped") {
					problems = append(problems, is.Message)
				}
			}
			o.Log.Info(fmt.Sprintf("editorial revision %d", rev), "problems", len(problems))
			prev, _ := json.MarshalIndent(doc, "", " ")
			_, err := o.runTask(ctx, st, taskSpec{
				ID: fmt.Sprintf("editorial-rev%d", rev), Role: agent.RoleEditorial, Stage: run.StageEditorial, Schema: "document",
				Objective: "Revise the draft report to fix every problem listed below, keeping everything that was sound.\n\nProblems:\n- " + strings.Join(problems, "\n- "),
				Inputs:    toArtifacts([]agentArtifact{{"previous draft", string(prev)}, {"synthesis", string(synJSON)}, {"research corpus", corpus}}),
				Artifact:  next,
			})
			if err != nil {
				o.Log.Warn("editorial revision failed; keeping previous draft", "error", firstLine(err.Error()))
				st.docPath = rel
				break
			}
		}
		rel = next
	}
	return "final draft " + st.docPath, nil
}

func readDocument(st *state, rel string) (report.Document, error) {
	var doc report.Document
	ok, err := st.run.ReadJSON(rel, &doc)
	if err == nil && !ok {
		err = fmt.Errorf("%s missing", rel)
	}
	return doc, err
}

// autoCiteCover is how much of a sentence must appear in a finding for
// AutoCite to cite it.
const autoCiteCover = 0.7

// readDraft reads an editorial draft. In quick mode, uncited sentences that
// restate a finding get that finding's citation (small local models rarely
// write citation markers themselves).
func (o *Orchestrator) readDraft(st *state, rel string) (report.Document, error) {
	doc, err := readDocument(st, rel)
	if err != nil || o.Config.Report.Mode != config.ModeQuick {
		return doc, err
	}
	if n := report.AutoCite(&doc, st.store, autoCiteCover); n > 0 {
		if !st.autoCited {
			o.Log.Info("citations matched to findings automatically", "draft", rel, "added", n)
		}
		st.autoCited = true
	}
	return doc, nil
}

// latestDocument finds the newest editorial draft in the run, preferring its
// polished version when the formatter produced one.
func latestDocument(st *state) string {
	rel := "report/document.json"
	for rev := 1; st.run.Exists(fmt.Sprintf("report/document-rev%d.json", rev)); rev++ {
		rel = fmt.Sprintf("report/document-rev%d.json", rev)
	}
	if p := strings.TrimSuffix(rel, ".json") + polishedSuffix; st.run.Exists(p) {
		return p
	}
	return rel
}

func (o *Orchestrator) metadata(st *state, doc report.Document) report.Metadata {
	m := st.run.Manifest()
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = st.plan.Title
	}
	return report.Metadata{
		Title: title, Subtitle: doc.Subtitle, ReportType: doc.ReportType, Topic: m.Topic,
		Date: st.asOf, RunID: m.ID, Author: o.Config.Report.Author,
		Generator: "Boku " + o.Version, PageSize: o.Config.Report.PageSize,
		ReferencesFile: run.Slug(title, 60) + ".references.json",
	}
}

func (o *Orchestrator) buildOptions(st *state, syn Synthesis) report.BuildOptions {
	var ws []string
	for _, w := range st.plan.Workstreams {
		ws = append(ws, w.Role)
	}
	var limits []string
	if st.lastFC != nil {
		limits = append(limits, st.lastFC.Limitations()...)
	}
	for _, f := range st.failed {
		limits = append(limits, fmt.Sprintf("The %s research workstream failed; its questions may be under-covered.", f))
	}
	if st.autoCited {
		limits = append(limits, "Some citations were matched to findings automatically by text similarity, because the local model did not cite them itself.")
	}
	for i, g := range syn.Gaps {
		if i == 6 {
			break
		}
		limits = append(limits, g)
	}
	status, rounds := "", 0
	if st.lastFC != nil {
		status, rounds = st.lastFC.Status, st.fcRounds
	}
	return report.BuildOptions{
		Charts: o.Config.Report.Charts, Diagrams: o.Config.Report.Diagrams,
		Layout: o.Config.Report.Layout, IncludeReferences: o.Config.Report.IncludeReferences,
		// Small local models mis-cite more often; drop bad citations instead of blocking.
		Lenient: o.Config.Report.Mode == config.ModeQuick,
		Method: &report.Method{
			Workstreams: uniqueStrings(ws), FactCheckRounds: rounds, FactCheckStatus: status,
			FreshnessDays: o.Config.Research.FreshnessDays, Limitations: limits,
		},
	}
}

func synthesisMarkdown(s Synthesis) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Synthesis: %s\n\n## Thesis\n\n%s\n\n## Insights\n\n", s.Title, s.Thesis)
	for _, in := range s.Insights {
		fmt.Fprintf(&b, "- **[%s, %s]** %s (%s)\n", in.Basis, in.Confidence, in.Statement, strings.Join(in.FindingIDs, ", "))
	}
	if len(s.Contradictions) > 0 {
		b.WriteString("\n## Contradictions\n\n")
		for _, c := range s.Contradictions {
			fmt.Fprintf(&b, "- %s → %s (%s)\n", c.Description, c.Resolution, strings.Join(c.FindingIDs, ", "))
		}
	}
	b.WriteString("\n## Outline\n\n")
	for i, sec := range s.Outline {
		fmt.Fprintf(&b, "%d. **%s** — %s (%s)\n", i+1, sec.Title, sec.Purpose, strings.Join(sec.FindingIDs, ", "))
	}
	if len(s.Gaps) > 0 {
		b.WriteString("\n## Gaps\n\n")
		for _, g := range s.Gaps {
			fmt.Fprintf(&b, "- %s\n", g)
		}
	}
	return b.String()
}

func uniqueStrings(xs []string) []string {
	var out []string
	for _, x := range xs {
		out = appendUnique(out, x)
	}
	return out
}
