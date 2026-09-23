package render

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
)

// PDFInfo is what the PDF quality gate needs to know about a rendered file.
type PDFInfo struct {
	Pages int
	// TextOps is the number of text-showing operators per page (1-based index = page).
	TextOps []int
	// EmptyPages lists pages whose only text is the running header/footer.
	EmptyPages []int
	Bytes      int64
}

var (
	objRe      = regexp.MustCompile(`(?s)(\d+) 0 obj\s*(.*?)endobj`)
	pageRe     = regexp.MustCompile(`/Type\s*/Page(?:[^s]|$)`)
	contentsRe = regexp.MustCompile(`/Contents\s*(?:(\d+) 0 R|\[([^\]]*)\])`)
	refRe      = regexp.MustCompile(`(\d+) 0 R`)
	streamRe   = regexp.MustCompile(`(?s)stream\r?\n(.*)\r?\nendstream`)
	textOpRe   = regexp.MustCompile(`(?:\)|>|\])\s*T[Jj]\b`)
)

// InspectPDF performs a lightweight structural check of a PDF produced by
// Chrome: page count and per-page text presence. It is not a general PDF parser.
//
// Chrome emits one text-show operator per glyph, so a page whose operator count
// does not exceed marginGlyphs (the running header/footer text, see
// MarginGlyphs) plus a small allowance for the page number has no body content.
func InspectPDF(path string, marginGlyphs int) (PDFInfo, error) {
	var info PDFInfo
	data, err := os.ReadFile(path)
	if err != nil {
		return info, err
	}
	info.Bytes = int64(len(data))
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return info, fmt.Errorf("%s is not a PDF", path)
	}
	objects := map[int][]byte{}
	var order []int
	for _, m := range objRe.FindAllSubmatchIndex(data, -1) {
		n, _ := strconv.Atoi(string(data[m[2]:m[3]]))
		objects[n] = data[m[4]:m[5]]
		order = append(order, n)
	}
	for _, n := range order {
		body := objects[n]
		dictEnd := bytes.Index(body, []byte("stream"))
		dict := body
		if dictEnd >= 0 {
			dict = body[:dictEnd]
		}
		if !pageRe.Match(dict) {
			continue
		}
		info.Pages++
		ops := 0
		if cm := contentsRe.FindSubmatch(dict); cm != nil {
			var refs [][]byte
			if len(cm[1]) > 0 {
				refs = [][]byte{cm[1]}
			} else {
				for _, r := range refRe.FindAllSubmatch(cm[2], -1) {
					refs = append(refs, r[1])
				}
			}
			for _, r := range refs {
				id, _ := strconv.Atoi(string(r))
				ops += countTextOps(objects[id])
			}
		}
		info.TextOps = append(info.TextOps, ops)
		// Page 1 is the cover, which has no running header or footer.
		if info.Pages > 1 && ops <= marginGlyphs+4 {
			info.EmptyPages = append(info.EmptyPages, info.Pages)
		}
	}
	if info.Pages == 0 {
		return info, fmt.Errorf("%s: no pages found", path)
	}
	return info, nil
}

func countTextOps(obj []byte) int {
	m := streamRe.FindSubmatch(obj)
	if m == nil {
		return 0
	}
	raw := m[1]
	if bytes.Contains(obj[:bytes.Index(obj, []byte("stream"))], []byte("FlateDecode")) {
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return 0
		}
		dec, _ := io.ReadAll(zr)
		raw = dec
	}
	return len(textOpRe.FindAll(raw, -1))
}
