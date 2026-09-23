package report

import (
	"strconv"
	"strings"
)

// Segment is a run of inline text. Exactly one of Text or Cites is set.
type Segment struct {
	Text   string
	Bold   bool
	Italic bool
	Cites  []int
}

// Inline splits built text into styled runs and citation markers. It supports
// **bold** and *italic*; everything else is literal text.
func Inline(s string) []Segment {
	var out []Segment
	for s != "" {
		i := strings.Index(s, citeOpen)
		if i < 0 {
			out = append(out, emphasis(s)...)
			break
		}
		out = append(out, emphasis(s[:i])...)
		rest := s[i+len(citeOpen):]
		j := strings.Index(rest, citeClose)
		if j < 0 {
			out = append(out, emphasis(rest)...)
			break
		}
		var nums []int
		for _, p := range strings.Split(rest[:j], ",") {
			if n, err := strconv.Atoi(p); err == nil {
				nums = append(nums, n)
			}
		}
		out = append(out, Segment{Cites: nums})
		s = rest[j+len(citeClose):]
	}
	return out
}

func emphasis(s string) []Segment {
	var out []Segment
	for s != "" {
		bi := strings.Index(s, "**")
		ii := indexSingleStar(s)
		switch {
		case bi >= 0 && (ii < 0 || bi <= ii):
			end := strings.Index(s[bi+2:], "**")
			if end < 0 {
				return append(out, Segment{Text: s})
			}
			if bi > 0 {
				out = append(out, Segment{Text: s[:bi]})
			}
			out = append(out, Segment{Text: s[bi+2 : bi+2+end], Bold: true})
			s = s[bi+2+end+2:]
		case ii >= 0:
			end := indexClosingStar(s[ii+1:])
			if end < 0 {
				return append(out, Segment{Text: s})
			}
			if ii > 0 {
				out = append(out, Segment{Text: s[:ii]})
			}
			out = append(out, Segment{Text: s[ii+1 : ii+1+end], Italic: true})
			s = s[ii+1+end+1:]
		default:
			return append(out, Segment{Text: s})
		}
	}
	return out
}

// indexClosingStar finds a single '*' that closes emphasis: preceded by a
// non-space character.
func indexClosingStar(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '*' && s[i-1] != ' ' && s[i-1] != '*' && (i+1 == len(s) || s[i+1] != '*') {
			return i
		}
	}
	return -1
}

// indexSingleStar finds a '*' that is not part of '**' and is attached to a
// word (so "5 * 3" is not treated as emphasis).
func indexSingleStar(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '*' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '*' {
			i++
			continue
		}
		if i > 0 && s[i-1] == '*' {
			continue
		}
		if i+1 < len(s) && s[i+1] != ' ' {
			return i
		}
	}
	return -1
}

// PlainText strips citation markers and emphasis, for checks and indexes.
func PlainText(s string) string {
	var b strings.Builder
	for _, seg := range Inline(s) {
		b.WriteString(seg.Text)
	}
	return b.String()
}

// HasCitation reports whether built text contains at least one citation.
func HasCitation(s string) bool { return strings.Contains(s, citeOpen) }
