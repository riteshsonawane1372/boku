package orchestrator

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/render"
	"github.com/riteshsonawane1372/boku/internal/report"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/internal/validation"
)

// stageReport builds the report model from the final draft and runs the
// editorial gate. The built model is written to report/report.json either way.
func (o *Orchestrator) stageReport(ctx context.Context, st *state) (string, error) {
	syn, err := o.loadSynthesis(st)
	if err != nil {
		return "", err
	}
	if st.docPath == "" {
		st.docPath = latestDocument(st)
	}
	doc, err := o.readDraft(st, st.docPath)
	if err != nil {
		return "", err
	}
	r, issues := report.Build(doc, st.store, o.metadata(st, doc), o.buildOptions(st, syn))
	st.report = r
	if err := st.run.WriteJSON("report/report.json", r); err != nil {
		return "", err
	}
	for _, w := range r.Warnings {
		o.Log.Warn(w)
	}
	g := validation.EditorialGate(r, issues)
	o.reportGate(st, g)
	if !g.Passed {
		// Keep a draft for inspection, but never publish it.
		if html, err := render.HTML(r); err == nil {
			_ = st.run.WriteFile("report/draft.html", html)
		}
		return "", &ErrBlocked{Gate: g.Name, Errors: g.Errors}
	}
	return fmt.Sprintf("%d sections, %d sources cited", len(r.Sections), len(r.Sources)), nil
}

// stageRender renders every requested format into the run directory, checks
// the PDF, and only then copies outputs to the output directory.
func (o *Orchestrator) stageRender(ctx context.Context, st *state) (*Outcome, error) {
	r := st.report
	slug := run.Slug(r.Metadata.Title, 60)
	outDir := o.Config.Output.Directory
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	// References always go to a JSON file next to the report; the document
	// names it so readers can resolve the numbered citations.
	refsDst := uniquePath(filepath.Join(outDir, slug+".references.json"), st.run.Manifest())
	r.Metadata.ReferencesFile = filepath.Base(refsDst)
	if err := st.run.WriteFile("report/references.json", render.ReferencesJSON(r)); err != nil {
		return nil, err
	}
	html, err := render.HTML(r)
	if err != nil {
		return nil, err
	}
	if err := st.run.WriteFile("report/report.html", html); err != nil {
		return nil, err
	}
	if err := st.run.WriteFile("report/report.md", render.Markdown(r)); err != nil {
		return nil, err
	}

	staged := map[string]string{} // format → path inside the run
	for _, f := range o.Config.Report.Formats {
		switch f {
		case "html":
			staged["html"] = st.run.Path("report", "report.html")
		case "md":
			staged["md"] = st.run.Path("report", "report.md")
		case "pdf":
			pdf := st.run.Path("output", slug+".pdf")
			if o.Printer == nil {
				return nil, fmt.Errorf("no PDF printer configured")
			}
			if err := o.Printer.Print(ctx, st.run.Path("report", "report.html"), pdf); err != nil {
				return nil, fmt.Errorf("render PDF: %w", err)
			}
			info, err := render.InspectPDF(pdf, render.MarginGlyphs(r.Metadata))
			if err != nil {
				return nil, fmt.Errorf("inspect PDF: %w", err)
			}
			g := validation.PDFGate(r, validation.PDFCheck{Path: pdf, Pages: info.Pages, EmptyPages: info.EmptyPages, HTML: string(html)})
			o.reportGate(st, g)
			if !g.Passed {
				return nil, &ErrBlocked{Gate: g.Name, Errors: g.Errors}
			}
			o.Log.Info("PDF rendered", "pages", info.Pages, "size", fmt.Sprintf("%.0f KB", float64(info.Bytes)/1024))
			staged["pdf"] = pdf
		}
	}

	out := &Outcome{RunDir: st.run.Dir}
	published := map[string]string{}
	for _, f := range o.Config.Report.Formats {
		src, ok := staged[f]
		if !ok {
			continue
		}
		dst := uniquePath(filepath.Join(outDir, slug+"."+f), st.run.Manifest())
		if err := copyFile(src, dst); err != nil {
			return nil, fmt.Errorf("publish %s: %w", f, err)
		}
		published[f] = dst
		out.Outputs = append(out.Outputs, dst)
	}
	if err := copyFile(st.run.Path("report", "references.json"), refsDst); err != nil {
		return nil, fmt.Errorf("publish references: %w", err)
	}
	published["refs"] = refsDst
	out.Outputs = append(out.Outputs, refsDst)
	_ = st.run.Update(func(m *run.Manifest) { m.Outputs = published })
	return out, nil
}

// uniquePath avoids overwriting a report from a different run: if dst exists
// and was not published by this run, the run ID's date-time is appended.
func uniquePath(dst string, m run.Manifest) string {
	for _, p := range m.Outputs {
		if p == dst {
			return dst
		}
	}
	if _, err := os.Stat(dst); err != nil {
		return dst
	}
	ext := filepath.Ext(dst)
	stamp := m.ID
	if i := strings.IndexByte(stamp, '-'); i > 0 && len(stamp) >= 17 {
		stamp = stamp[:17]
	}
	return strings.TrimSuffix(dst, ext) + "-" + stamp + ext
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Render rebuilds the report and its outputs from a run's existing artifacts
// without calling any agent — useful after editing a draft, the stylesheet or
// the renderer. The run must have completed the editorial stage.
func (o *Orchestrator) Render(ctx context.Context, r *run.Run) (*Outcome, error) {
	m := r.Manifest()
	st := &state{run: r, asOf: m.CreatedAt, ingested: map[string]bool{}}
	if err := o.loadEvidence(st); err != nil {
		return nil, err
	}
	var plan Plan
	if ok, err := r.ReadJSON("plan.json", &plan); err != nil || !ok {
		return nil, fmt.Errorf("run has no plan.json; nothing to render")
	}
	st.plan = &plan
	for round := 1; r.Exists(fmt.Sprintf("factcheck/round-%d.json", round)); round++ {
		var fc validation.FactCheck
		if _, err := r.ReadJSON(fmt.Sprintf("factcheck/round-%d.json", round), &fc); err != nil {
			return nil, err
		}
		st.lastFC, st.fcRounds = &fc, round
	}
	if !r.Exists("synthesis/synthesis.json") || !r.Exists("report/document.json") {
		return nil, fmt.Errorf("run has not reached the editorial stage; use `boku resume` instead")
	}
	st.docPath = latestDocument(st)
	o.Log.Info("rendering from artifacts", "draft", st.docPath)
	st.run.StageStart(run.StageReport)
	detail, err := o.stageReport(ctx, st)
	if err != nil {
		st.run.StageFail(run.StageReport, run.StatusBlocked, err)
		return nil, err
	}
	st.run.StageDone(run.StageReport, detail)
	st.run.StageStart(run.StageRender)
	out, err := o.stageRender(ctx, st)
	if err != nil {
		st.run.StageFail(run.StageRender, run.StatusFailed, err)
		return nil, err
	}
	st.run.StageDone(run.StageRender, strings.Join(out.Outputs, ", "))
	_ = r.Update(func(m *run.Manifest) { m.Status, m.Error = run.StatusCompleted, "" })
	out.Gates = st.gates
	return out, nil
}
