package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var roles = []string{"planner", "primary", "market", "technical", "financial", "competitive", "case-study", "fact-checker", "synthesizer", "editorial", "formatter"}

func TestEveryRoleRenders(t *testing.T) {
	lib, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		s, err := lib.System(role, Vars{Today: "2026-09-23", FreshnessDays: 90, Depth: "deep", Mode: "full"})
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

func TestExplainerAddOn(t *testing.T) {
	lib, _ := Load("")
	for _, codebase := range []bool{false, true} {
		for _, role := range roles {
			s, err := lib.System(role, Vars{Today: "2026-09-23", FreshnessDays: 90, Depth: "standard", Mode: "explainer", Codebase: codebase})
			if err != nil {
				t.Fatalf("%s: %v", role, err)
			}
			if !strings.Contains(s, "produces an explainer") || strings.Contains(s, "{{") {
				t.Errorf("%s: explainer add-on missing or unrendered", role)
			}
			if strings.Contains(s, "local codebase") != codebase {
				t.Errorf("%s (codebase=%v): codebase section wrong", role, codebase)
			}
		}
	}
	s, _ := lib.System("editorial", Vars{Today: "2026-09-23", Mode: "explainer"})
	if !strings.Contains(s, "Writing an explainer") || strings.Contains(s, "Planning an explainer") || strings.Contains(s, "explainer report**") {
		t.Error("editorial explainer prompt picks up the wrong sections")
	}
	full, _ := lib.System("editorial", Vars{Today: "2026-09-23", Mode: "full"})
	if strings.Contains(full, "explainer") {
		t.Error("explainer add-on leaked into full mode")
	}
}

func TestWhitepaperAddOn(t *testing.T) {
	lib, _ := Load("")
	for _, role := range roles {
		s, err := lib.System(role, Vars{Today: "2026-09-23", FreshnessDays: 90, Depth: "deep", Mode: "whitepaper"})
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if !strings.Contains(s, "produces a whitepaper") || strings.Contains(s, "{{") || strings.Contains(s, "produces an explainer") {
			t.Errorf("%s: whitepaper add-on missing, unrendered or mixed with explainer", role)
		}
	}
	s, _ := lib.System("editorial", Vars{Today: "2026-09-23", Mode: "whitepaper"})
	if !strings.Contains(s, "Writing a whitepaper") || strings.Contains(s, "Planning a whitepaper") || strings.Contains(s, "whitepaper report**") {
		t.Error("editorial whitepaper prompt picks up the wrong sections")
	}
}

func TestSchemasAreValidJSON(t *testing.T) {
	lib, _ := Load("")
	for _, name := range []string{"planner", "research", "factcheck", "synthesis", "document", "formatter"} {
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
