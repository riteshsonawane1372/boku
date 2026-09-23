// Package config loads and validates Boku configuration.
//
// Configuration is resolved in three layers: built-in defaults, an optional
// YAML file, and CLI flag overrides applied by the caller.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Depth controls how much research Boku performs.
type Depth string

const (
	DepthQuick    Depth = "quick"
	DepthStandard Depth = "standard"
	DepthDeep     Depth = "deep"
)

type Config struct {
	Project  Project  `yaml:"project" json:"project"`
	Agents   Agents   `yaml:"agents" json:"agents"`
	Research Research `yaml:"research" json:"research"`
	Report   Report   `yaml:"report" json:"report"`
	Output   Output   `yaml:"output" json:"output"`
}

type Project struct {
	Name string `yaml:"name" json:"name"`
}

type Agents struct {
	// Provider selects the agent runtime. Only "claude-code" exists today.
	Provider string `yaml:"provider" json:"provider"`
	// Command is the Claude Code executable.
	Command string `yaml:"command" json:"command"`
	// Model is passed to the provider; empty uses the provider default.
	Model string `yaml:"model" json:"model"`
	// RoleModels overrides Model for specific roles (e.g. editorial: opus).
	RoleModels map[string]string `yaml:"role_models" json:"role_models,omitempty"`
	// MaxParallel bounds concurrently running agents.
	MaxParallel int `yaml:"max_parallel" json:"max_parallel"`
	// MaxAgents bounds the number of research workstreams the planner may create.
	MaxAgents int `yaml:"max_agents" json:"max_agents"`
	// MaxRetries is the number of retries after a failed agent attempt.
	MaxRetries int `yaml:"max_retries" json:"max_retries"`
	// Timeout bounds a single agent invocation.
	Timeout Duration `yaml:"timeout" json:"timeout"`
	// MaxCostUSD stops launching new agents once spend reaches it. 0 = unlimited.
	MaxCostUSD float64 `yaml:"max_cost_usd" json:"max_cost_usd"`
	// EnvPassthrough lists extra environment variables agents may see
	// (for example AWS_PROFILE when using Claude Code via Bedrock).
	EnvPassthrough []string `yaml:"env_passthrough" json:"env_passthrough,omitempty"`
}

type Research struct {
	Depth Depth `yaml:"depth" json:"depth"`
	// FreshnessDays is the window within which dated information counts as current.
	FreshnessDays int `yaml:"freshness_days" json:"freshness_days"`
	// MaxIterations bounds fact-check → follow-up research rounds.
	MaxIterations int `yaml:"max_iterations" json:"max_iterations"`
	// MinSources is the minimum number of distinct sources the research gate requires.
	// 0 derives it from depth.
	MinSources int `yaml:"min_sources" json:"min_sources"`
	// Sources are hints (domains or source kinds) passed to the planner.
	Sources []string `yaml:"sources" json:"sources,omitempty"`
}

type Report struct {
	// Formats to produce: pdf, html, md.
	Formats   []string `yaml:"formats" json:"formats"`
	Citations bool     `yaml:"citations" json:"citations"`
	Charts    bool     `yaml:"charts" json:"charts"`
	Diagrams  bool     `yaml:"diagrams" json:"diagrams"`
	// PageSize is A4 or Letter.
	PageSize string `yaml:"page_size" json:"page_size"`
	// Chrome is the browser used to print PDFs; empty autodetects.
	Chrome string `yaml:"chrome" json:"chrome,omitempty"`
	// Author appears on the cover.
	Author string `yaml:"author" json:"author,omitempty"`
}

type Output struct {
	Directory string `yaml:"directory" json:"directory"`
	RunsDir   string `yaml:"runs_directory" json:"runs_directory"`
	// PromptsDir overrides the embedded prompts with files from disk.
	PromptsDir string `yaml:"prompts_directory" json:"prompts_directory,omitempty"`
}

// Duration is a time.Duration that marshals as a Go duration string ("15m").
type Duration time.Duration

func (d Duration) String() string { return time.Duration(d).String() }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", n.Value, err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

func (d Duration) MarshalJSON() ([]byte, error) { return []byte(`"` + d.String() + `"`), nil }

func (d *Duration) UnmarshalJSON(b []byte) error {
	v, err := time.ParseDuration(strings.Trim(string(b), `"`))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Project: Project{Name: "boku"},
		Agents: Agents{
			Provider:    "claude-code",
			Command:     "claude",
			MaxParallel: 4,
			MaxAgents:   6,
			MaxRetries:  2,
			Timeout:     Duration(20 * time.Minute),
		},
		Research: Research{
			Depth:         DepthStandard,
			FreshnessDays: 365,
			MaxIterations: 2,
		},
		Report: Report{
			Formats:   []string{"pdf"},
			Citations: true,
			Charts:    true,
			Diagrams:  true,
			PageSize:  "A4",
		},
		Output: Output{
			Directory: "./reports",
			RunsDir:   "./runs",
		},
	}
}

// Load reads a YAML file on top of the defaults. A missing path returns defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, cfg.Validate()
}

// Validate reports every invalid setting at once.
func (c Config) Validate() error {
	var errs []error
	if c.Agents.Provider != "claude-code" {
		errs = append(errs, fmt.Errorf("agents.provider %q is not supported (supported: claude-code)", c.Agents.Provider))
	}
	if c.Agents.MaxParallel < 1 {
		errs = append(errs, errors.New("agents.max_parallel must be >= 1"))
	}
	if c.Agents.MaxAgents < 1 {
		errs = append(errs, errors.New("agents.max_agents must be >= 1"))
	}
	if c.Agents.MaxRetries < 0 {
		errs = append(errs, errors.New("agents.max_retries must be >= 0"))
	}
	if c.Agents.Timeout <= 0 {
		errs = append(errs, errors.New("agents.timeout must be positive"))
	}
	if c.Agents.MaxCostUSD < 0 {
		errs = append(errs, errors.New("agents.max_cost_usd must be >= 0"))
	}
	switch c.Research.Depth {
	case DepthQuick, DepthStandard, DepthDeep:
	default:
		errs = append(errs, fmt.Errorf("research.depth %q must be quick, standard or deep", c.Research.Depth))
	}
	if c.Research.FreshnessDays < 1 {
		errs = append(errs, errors.New("research.freshness_days must be >= 1"))
	}
	if c.Research.MaxIterations < 0 {
		errs = append(errs, errors.New("research.max_iterations must be >= 0"))
	}
	if len(c.Report.Formats) == 0 {
		errs = append(errs, errors.New("report.formats must not be empty"))
	}
	for _, f := range c.Report.Formats {
		switch f {
		case "pdf", "html", "md":
		default:
			errs = append(errs, fmt.Errorf("report.formats: unknown format %q (pdf, html, md)", f))
		}
	}
	switch strings.ToLower(c.Report.PageSize) {
	case "a4", "letter":
	default:
		errs = append(errs, fmt.Errorf("report.page_size %q must be A4 or Letter", c.Report.PageSize))
	}
	return errors.Join(errs...)
}

// MinSourcesFor returns the research gate's source minimum.
func (c Config) MinSourcesFor() int {
	if c.Research.MinSources > 0 {
		return c.Research.MinSources
	}
	switch c.Research.Depth {
	case DepthQuick:
		return 5
	case DepthDeep:
		return 20
	default:
		return 10
	}
}

// ModelFor returns the model configured for a role.
func (c Config) ModelFor(role string) string {
	if m, ok := c.Agents.RoleModels[role]; ok && m != "" {
		return m
	}
	return c.Agents.Model
}

// ParseFreshness accepts "30d", "12w", "6m", "1y" or a plain number of days.
func ParseFreshness(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, errors.New("empty freshness")
	}
	unit := 1
	switch s[len(s)-1] {
	case 'd':
		s = s[:len(s)-1]
	case 'w':
		unit, s = 7, s[:len(s)-1]
	case 'm':
		unit, s = 30, s[:len(s)-1]
	case 'y':
		unit, s = 365, s[:len(s)-1]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid freshness %q (use e.g. 30d, 12w, 6m, 1y)", s)
	}
	return n * unit, nil
}
