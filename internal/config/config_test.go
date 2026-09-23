package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boku.yaml")
	_ = os.WriteFile(path, []byte(`
agents:
  max_parallel: 2
  timeout: 5m
  role_models: {editorial: opus}
research:
  depth: deep
report:
  formats: [pdf, md]
`), 0o644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.MaxParallel != 2 || time.Duration(cfg.Agents.Timeout) != 5*time.Minute || cfg.Research.Depth != DepthDeep {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.Agents.MaxAgents != 6 || cfg.Output.Directory != "./reports" {
		t.Error("defaults lost for unspecified fields")
	}
	if cfg.ModelFor("editorial") != "opus" || cfg.ModelFor("planner") != "" {
		t.Error("role model override wrong")
	}
	if cfg.MinSourcesFor() != 20 {
		t.Errorf("deep min sources = %d", cfg.MinSourcesFor())
	}
}

func TestLoadRejectsUnknownAndInvalid(t *testing.T) {
	dir := t.TempDir()
	unknown := filepath.Join(dir, "a.yaml")
	_ = os.WriteFile(unknown, []byte("agents:\n  max_paralel: 2\n"), 0o644)
	if _, err := Load(unknown); err == nil || !strings.Contains(err.Error(), "max_paralel") {
		t.Errorf("typo not reported: %v", err)
	}
	bad := filepath.Join(dir, "b.yaml")
	_ = os.WriteFile(bad, []byte("agents:\n  provider: gpt\n  max_parallel: 0\nresearch:\n  depth: extreme\nreport:\n  formats: [docx]\n"), 0o644)
	_, err := Load(bad)
	for _, want := range []string{"provider", "max_parallel", "depth", "docx"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("missing explicit config file should error")
	}
}

func TestParseFreshness(t *testing.T) {
	for in, want := range map[string]int{"30d": 30, "2w": 14, "6m": 180, "1y": 365, "45": 45} {
		got, err := ParseFreshness(in)
		if err != nil || got != want {
			t.Errorf("ParseFreshness(%q) = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "d", "-3d", "30x", "abc"} {
		if _, err := ParseFreshness(in); err == nil {
			t.Errorf("ParseFreshness(%q) should fail", in)
		}
	}
}

func TestExamplesAreValid(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	if len(paths) == 0 {
		t.Fatal("no example configs found")
	}
	for _, p := range paths {
		if _, err := Load(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}
