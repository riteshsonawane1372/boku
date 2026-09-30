// Package prompts holds the agent prompts and output schemas. The Markdown and
// JSON files in this directory are embedded into the binary; a directory with
// the same layout can override them at runtime (output.prompts_directory).
//
// A role's system prompt is common.md, then researcher.md for research roles,
// then <role>.md. Prompts are Go text/templates with these fields:
//
//	{{.Today}}          current date, YYYY-MM-DD
//	{{.FreshnessDays}}  freshness window in days
//	{{.Depth}}          quick | standard | deep
//	{{.Mode}}           full | short | quick
package prompts

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed *.md schemas/*.json
var embedded embed.FS

// Vars are the template fields available in prompts.
type Vars struct {
	Today         string
	FreshnessDays int
	Depth         string
	Mode          string
}

// Library resolves prompts and schemas.
type Library struct {
	fsys fs.FS
}

// Load returns the embedded library, or one backed by dir when dir is set.
// Files missing from dir fall back to the embedded versions.
func Load(dir string) (*Library, error) {
	if dir == "" {
		return &Library{fsys: embedded}, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "common.md")); err != nil {
		return nil, fmt.Errorf("prompts directory %s: %w", dir, err)
	}
	return &Library{fsys: overlay{os.DirFS(dir), embedded}}, nil
}

type overlay struct{ top, bottom fs.FS }

func (o overlay) Open(name string) (fs.File, error) {
	if f, err := o.top.Open(name); err == nil {
		return f, nil
	}
	return o.bottom.Open(name)
}

// researchRoles share researcher.md.
var researchRoles = map[string]bool{
	"primary": true, "market": true, "technical": true, "financial": true, "competitive": true, "case-study": true,
}

// System renders the system prompt for a role.
func (l *Library) System(role string, v Vars) (string, error) {
	files := []string{"common.md"}
	if researchRoles[role] {
		files = append(files, "researcher.md")
	}
	files = append(files, role+".md")
	var buf bytes.Buffer
	for i, name := range files {
		b, err := fs.ReadFile(l.fsys, name)
		if err != nil {
			return "", fmt.Errorf("prompt for role %q: %w", role, err)
		}
		t, err := template.New(name).Option("missingkey=error").Parse(string(b))
		if err != nil {
			return "", fmt.Errorf("prompt %s: %w", name, err)
		}
		if i > 0 {
			buf.WriteString("\n\n")
		}
		if err := t.Execute(&buf, v); err != nil {
			return "", fmt.Errorf("prompt %s: %w", name, err)
		}
	}
	return buf.String(), nil
}

// Schema returns a compacted JSON Schema by name (planner, research, factcheck,
// synthesis, document, formatter).
func (l *Library) Schema(name string) (json.RawMessage, error) {
	b, err := fs.ReadFile(l.fsys, "schemas/"+name+".json")
	if err != nil {
		return nil, fmt.Errorf("schema %q: %w", name, err)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		return nil, fmt.Errorf("schema %q: %w", name, err)
	}
	return out.Bytes(), nil
}

// Version returns a short content hash of the files making up a role's
// prompt, recorded in the run manifest for reproducibility.
func (l *Library) Version(role string) string {
	h := sha256.New()
	files := []string{"common.md"}
	if researchRoles[role] {
		files = append(files, "researcher.md")
	}
	for _, name := range append(files, role+".md") {
		b, _ := fs.ReadFile(l.fsys, name)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
