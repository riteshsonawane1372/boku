package report

import (
	"regexp"
	"strings"

	"github.com/riteshsonawane1372/boku/internal/research"
)

var (
	wordRe = regexp.MustCompile(`[a-z0-9][a-z0-9.%-]*`)
)

// AutoCite adds finding citations to uncited sentences that restate a
// usable finding's claim. Small local models often copy findings nearly
// verbatim but do not write [F001] markers; this recovers those citations
// deterministically. A sentence is matched only when at least minCover of
// its significant words (and at least four) appear in one claim. It returns
// the number of citations added. Used for quick reports only.
func AutoCite(doc *Document, store *research.Store, minCover float64) int {
	type cand struct {
		id    string
		words map[string]bool
	}
	var cands []cand
	for _, f := range store.Usable() {
		cands = append(cands, cand{f.ID, words(f.Claim + " " + f.Evidence)})
	}
	added := 0
	cite := func(s *string) {
		if markerRe.MatchString(*s) {
			return
		}
		var b strings.Builder
		for _, sent := range splitSentences(*s) {
			ws := words(sent)
			best, bestCover := "", 0.0
			if len(ws) >= 4 {
				for _, c := range cands {
					hit := 0
					for w := range ws {
						if c.words[w] {
							hit++
						}
					}
					if cover := float64(hit) / float64(len(ws)); cover > bestCover {
						best, bestCover = c.id, cover
					}
				}
			}
			if best != "" && bestCover >= minCover {
				trail := sent[len(strings.TrimRight(sent, " \t\n")):]
				body := strings.TrimRight(sent, " \t\n")
				end := ""
				if n := len(body); n > 0 && strings.ContainsRune(".!?", rune(body[n-1])) {
					body, end = body[:n-1], body[n-1:]
				}
				sent = body + " [" + best + "]" + end + trail
				added++
			}
			b.WriteString(sent)
		}
		*s = b.String()
	}
	for i := range doc.ExecutiveSummary {
		cite(&doc.ExecutiveSummary[i])
	}
	for i := range doc.KeyFindings {
		cite(&doc.KeyFindings[i].Detail)
	}
	for si := range doc.Sections {
		for bi := range doc.Sections[si].Blocks {
			bl := &doc.Sections[si].Blocks[bi]
			switch bl.Type {
			case BlockParagraph, BlockCallout:
				cite(&bl.Text)
			case BlockBullets:
				for ii := range bl.Items {
					cite(&bl.Items[ii])
				}
			}
		}
	}
	for i := range doc.Conclusion {
		cite(&doc.Conclusion[i])
	}
	return added
}

var citeStop = map[string]bool{
	"the": true, "and": true, "are": true, "for": true, "with": true, "that": true, "this": true, "but": true,
	"can": true, "its": true, "has": true, "have": true, "was": true, "were": true, "from": true, "into": true,
	"not": true, "only": true, "also": true, "more": true, "than": true, "such": true, "which": true, "their": true,
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		w = strings.TrimRight(w, ".-")
		if len(w) >= 3 && !citeStop[w] {
			m[w] = true
		}
	}
	return m
}

// splitSentences splits after '.', '!' or '?' followed by whitespace, so
// decimals ("$4.2") stay inside their sentence. Pieces keep their trailing
// whitespace and concatenate back to s.
func splitSentences(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s)-1; i++ {
		if strings.ContainsRune(".!?", rune(s[i])) && (s[i+1] == ' ' || s[i+1] == '\n') {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\n') {
				j++
			}
			out = append(out, s[start:j])
			start, i = j, j-1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
