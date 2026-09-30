package render

import (
	"bytes"
	"encoding/json"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// refEntry is one numbered reference, with short keys: the references file is
// meant to be cheap to hand to another model or tool.
type refEntry struct {
	N         int    `json:"n"`
	Title     string `json:"title"`
	Publisher string `json:"publisher,omitempty"`
	URL       string `json:"url"`
	Published string `json:"published,omitempty"`
	Accessed  string `json:"accessed"`
	Tier      int    `json:"tier"`
}

type evidenceEntry struct {
	ID      string `json:"id"`
	Claim   string `json:"claim"`
	Status  string `json:"status"`
	AsOf    string `json:"as_of,omitempty"`
	Sources []int  `json:"refs"`
}

// ReferencesJSON renders the report's numbered sources and the evidence
// register as compact JSON, one entry per line. Citation numbers in the
// PDF/HTML/Markdown ([3]) are the "n" values here.
func ReferencesJSON(r *report.Report) []byte {
	var b bytes.Buffer
	head, _ := json.Marshal(map[string]string{
		"title": r.Metadata.Title, "topic": r.Metadata.Topic, "run_id": r.Metadata.RunID,
		"date": r.Metadata.Date.Format("2006-01-02"), "generator": r.Metadata.Generator,
	})
	b.Write(head[:len(head)-1])
	b.WriteString(",\n\"references\":[")
	for i, c := range r.Sources {
		if i > 0 {
			b.WriteByte(',')
		}
		e := refEntry{N: c.Number, Title: c.Title, Publisher: c.Publisher, URL: c.URL, Published: fmtISO(c.Published), Accessed: c.Accessed.Format("2006-01-02"), Tier: c.Tier}
		line, _ := json.Marshal(e)
		b.WriteString("\n")
		b.Write(line)
	}
	b.WriteString("\n],\n\"evidence\":[")
	for i, e := range r.Evidence {
		if i > 0 {
			b.WriteByte(',')
		}
		line, _ := json.Marshal(evidenceEntry{ID: e.ID, Claim: e.Claim, Status: e.Status, AsOf: e.AsOf, Sources: e.Sources})
		b.WriteString("\n")
		b.Write(line)
	}
	b.WriteString("\n]}\n")
	return b.Bytes()
}
