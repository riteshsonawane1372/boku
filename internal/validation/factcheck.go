// Package validation applies fact-check results to the evidence store and
// runs the quality gates that decide whether a report may be published.
package validation

import (
	"fmt"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/research"
)

// FactCheck is the fact checker's output (prompts/schemas/factcheck.json).
type FactCheck struct {
	Status              string                  `json:"status"`
	Summary             string                  `json:"summary"`
	Verdicts            []Verdict               `json:"verdicts"`
	Issues              []FactIssue             `json:"issues"`
	ClaimsToVerify      []string                `json:"claims_to_verify"`
	MissingSources      []string                `json:"missing_sources"`
	Corrections         research.ResearchOutput `json:"corrections"`
	RecommendedResearch []FollowUp              `json:"recommended_research"`
}

type Verdict struct {
	FindingID string `json:"finding_id"`
	Verdict   string `json:"verdict"`
	Note      string `json:"note"`
}

type FactIssue struct {
	Severity       string   `json:"severity"`
	Type           string   `json:"type"`
	FindingIDs     []string `json:"finding_ids"`
	Description    string   `json:"description"`
	Recommendation string   `json:"recommendation"`
}

type FollowUp struct {
	Role       string   `json:"role"`
	Objective  string   `json:"objective"`
	Questions  []string `json:"questions"`
	FindingIDs []string `json:"finding_ids"`
}

const (
	FactPass          = "pass"
	FactNeedsRevision = "needs_revision"
	FactFail          = "fail"
)

// ApplyVerdicts updates finding review states. It is idempotent: applying the
// same round twice gives the same store. Returns the number of findings changed
// and warnings for verdicts referencing unknown findings.
func ApplyVerdicts(store *research.Store, fc FactCheck, round int) (changed int, warnings []string) {
	for _, v := range fc.Verdicts {
		f := store.Finding(strings.TrimSpace(v.FindingID))
		if f == nil {
			warnings = append(warnings, fmt.Sprintf("fact-check verdict for unknown finding %q ignored", v.FindingID))
			continue
		}
		before := f.Review
		switch v.Verdict {
		case "verified", "supported":
			f.Review = research.Review{Status: research.ReviewAccepted, Verdict: v.Verdict, Note: v.Note, Round: round}
		case "weak":
			f.Review = research.Review{Status: research.ReviewFlagged, Verdict: v.Verdict, Note: v.Note, Round: round}
		case "unsupported", "contradicted":
			f.Review = research.Review{Status: research.ReviewRejected, Verdict: v.Verdict, Note: v.Note, Round: round}
		case "outdated":
			f.Review = research.Review{Status: research.ReviewAccepted, Verdict: v.Verdict, Note: v.Note, Round: round}
			if f.Temporal == research.TemporalCurrent || f.Temporal == research.TemporalUnknown {
				f.Temporal = research.TemporalHistorical
			}
		default:
			warnings = append(warnings, fmt.Sprintf("unknown verdict %q for %s ignored", v.Verdict, v.FindingID))
			continue
		}
		if f.Review != before {
			changed++
		}
	}
	return changed, warnings
}

// AcceptPending marks findings the fact checker did not object to as accepted.
func AcceptPending(store *research.Store) {
	for i := range store.Findings {
		if store.Findings[i].Review.Status == research.ReviewPending {
			store.Findings[i].Review.Status = research.ReviewAccepted
		}
	}
}

// CriticalIssues returns issues of critical severity.
func (fc FactCheck) CriticalIssues() []FactIssue {
	var out []FactIssue
	for _, is := range fc.Issues {
		if is.Severity == "critical" {
			out = append(out, is)
		}
	}
	return out
}

// Unverified lists what the fact checker could not verify and the report must
// flag prominently: unresolved critical issues, and the summary of a failed
// check.
func (fc FactCheck) Unverified() []string {
	var out []string
	if fc.Status == FactFail && strings.TrimSpace(fc.Summary) != "" {
		out = append(out, strings.TrimSpace(fc.Summary))
	}
	for _, is := range fc.CriticalIssues() {
		out = append(out, strings.TrimSpace(is.Description))
	}
	return dedupe(out, 12)
}

// Limitations turns unresolved fact-check output into reader-facing caveats.
func (fc FactCheck) Limitations() []string {
	var out []string
	for _, is := range fc.Issues {
		// Critical issues are shown in the report's "not verified" notice.
		if is.Severity == "minor" || is.Severity == "critical" {
			continue
		}
		out = append(out, strings.TrimSpace(is.Description))
	}
	for _, c := range fc.ClaimsToVerify {
		out = append(out, "Unverified: "+strings.TrimSpace(c))
	}
	return dedupe(out, 12)
}

func dedupe(xs []string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		k := strings.ToLower(x)
		if x == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, x)
		if len(out) == max {
			break
		}
	}
	return out
}
