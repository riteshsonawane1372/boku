package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/report"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/internal/validation"
)

const polishedSuffix = "-polished.json"

// passage is one piece of draft text sent to the formatter.
type passage struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Problems []string `json:"problems,omitempty"`
	set      func(string)
}

// polish sends the draft's passages that have style problems (generic
// phrasing, Markdown, repeated sentences) to the local formatter and writes
// the corrected draft to rel. A rewrite is accepted only if it keeps exactly
// the same citation markers and numbers, so the formatter cannot change facts.
func (o *Orchestrator) polish(ctx context.Context, st *state, doc report.Document, rel string) error {
	all := passages(&doc)
	seen := map[string]bool{}
	var flagged []*passage
	for _, p := range all {
		probs := validation.StyleProblems(p.Text)
		for _, s := range validation.Sentences(p.Text) {
			if seen[s] {
				probs = append(probs, "repeats a sentence used earlier in the report")
				break
			}
		}
		for _, s := range validation.Sentences(p.Text) {
			seen[s] = true
		}
		if len(probs) > 0 {
			p.Problems = probs
			flagged = append(flagged, p)
		}
	}
	if len(flagged) == 0 {
		return errors.New("no passages to polish")
	}
	o.Log.Info("local formatter polishing draft", "passages", len(flagged))
	in, _ := json.MarshalIndent(flagged, "", " ")
	raw, err := o.runTask(ctx, st, taskSpec{
		ID: "formatter-" + strings.TrimSuffix(strings.TrimPrefix(rel, "report/"), ".json"), Role: agent.RoleFormatter,
		Stage: run.StageEditorial, Schema: "formatter",
		Objective: "Fix the listed style problems in each passage without changing any fact, number or citation marker.",
		Inputs:    []agent.Artifact{{Name: "passages", Content: string(in)}},
		Artifact:  "agents/" + strings.TrimSuffix(strings.TrimPrefix(rel, "report/"), ".json") + ".formatter.json",
	})
	if err != nil {
		o.Log.Warn("local formatter failed; continuing without it", "error", firstLine(err.Error()))
		return err
	}
	var out struct {
		Passages []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"passages"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	byID := map[string]*passage{}
	for _, p := range flagged {
		byID[p.ID] = p
	}
	applied := 0
	for _, r := range out.Passages {
		p, ok := byID[r.ID]
		if !ok || strings.TrimSpace(r.Text) == "" || !sameFacts(p.Text, r.Text) {
			continue
		}
		p.set(strings.TrimSpace(r.Text))
		applied++
	}
	o.Log.Info("local formatter finished", "rewritten", applied, "rejected", len(flagged)-applied)
	if applied == 0 {
		return errors.New("formatter changed nothing usable")
	}
	return st.run.WriteJSON(rel, doc)
}

// passages returns every free-text field of the document with a setter.
func passages(doc *report.Document) []*passage {
	var out []*passage
	add := func(id string, s *string) {
		if strings.TrimSpace(*s) != "" {
			out = append(out, &passage{ID: id, Text: *s, set: func(v string) { *s = v }})
		}
	}
	for i := range doc.ExecutiveSummary {
		add(fmt.Sprintf("summary-%d", i+1), &doc.ExecutiveSummary[i])
	}
	for i := range doc.KeyFindings {
		add(fmt.Sprintf("finding-%d", i+1), &doc.KeyFindings[i].Detail)
	}
	for si := range doc.Sections {
		for bi := range doc.Sections[si].Blocks {
			b := &doc.Sections[si].Blocks[bi]
			id := fmt.Sprintf("s%d-b%d", si+1, bi+1)
			switch b.Type {
			case report.BlockParagraph, report.BlockCallout:
				add(id, &b.Text)
			case report.BlockBullets:
				for ii := range b.Items {
					add(fmt.Sprintf("%s-i%d", id, ii+1), &b.Items[ii])
				}
			}
		}
	}
	for i := range doc.Conclusion {
		add(fmt.Sprintf("conclusion-%d", i+1), &doc.Conclusion[i])
	}
	return out
}

var (
	findingIDRe = regexp.MustCompile(`F\d{1,4}`)
	numberRe    = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
)

// sameFacts reports whether b keeps exactly a's finding citations and numbers.
func sameFacts(a, b string) bool {
	return sameMultiset(findingIDRe.FindAllString(a, -1), findingIDRe.FindAllString(b, -1)) &&
		sameMultiset(numberRe.FindAllString(a, -1), numberRe.FindAllString(b, -1))
}

func sameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
