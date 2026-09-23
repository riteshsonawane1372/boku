package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var roles = []string{"planner", "primary", "market", "technical", "financial", "competitive", "case-study", "fact-checker", "synthesizer", "editorial"}

func TestEveryRoleRenders(t *testing.T) {
	lib, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		s, err := lib.System(role, Vars{Today: "2026-09-23", FreshnessDays: 90, Depth: "deep"})
		if err != nil {
			t.Errorf("%s: %v", role, err)
			continue
		}
		if !strings.Contains(s, "2026-09-23") || !strings.Contains(s, "90 days") || strings.Contains(s, "{{") {
			t.Errorf("%s: template not rendered", role)
		}
		if researchRoles[role] != strings.Contains(s, "research agent") {
			t.Errorf("%s: researcher.md inclusion wrong", role)
		}
		if lib.Version(role) == "" {
			t.Errorf("%s: empty version", role)
		}
	}
	if _, err := lib.System("astrologer", Vars{}); err == nil {
		t.Error("unknown role should fail")
	}
}

func TestSchemasAreValidJSON(t *testing.T) {
	lib, _ := Load("")
	for _, name := range []string{"planner", "research", "factcheck", "synthesis", "document"} {
		raw, err := lib.Schema(name)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil || v["type"] != "object" {
			t.Errorf("%s: invalid schema", name)
		}
	}
}

func TestOverrideDirectory(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "common.md"), []byte("CUSTOM {{.Today}}"), 0o644)
	lib, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := lib.System("editorial", Vars{Today: "X"})
	if err != nil || !strings.HasPrefix(s, "CUSTOM X") || !strings.Contains(s, "role: editor") {
		t.Errorf("override not layered over embedded prompts: %v %.60q", err, s)
	}
	def, _ := Load("")
	if lib.Version("editorial") == def.Version("editorial") {
		t.Error("version should change with prompt content")
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing override dir should fail")
	}
}
