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

// Mode is the kind of report to produce.
type Mode string

const (
	// ModeFull is the default: full research programme, fact-checking and a
	// typeset report with cover and contents.
	ModeFull Mode = "full"
	// ModeShort is a short search report: fewer workstreams, one fact-check
	// round, a compact document of a few pages.
	ModeShort Mode = "short"
	// ModeQuick runs every agent on the local model (Ollama) with Boku's own
	// web search, skips fact-checking and produces a compact report. It spends
	// no Claude Code tokens.
	ModeQuick Mode = "quick"
	// ModeExplainer produces a visual explainer instead of a research report:
	// plain-language concepts, diagrams of how the parts fit together and
	// step-by-step walkthroughs. With research.codebase set, the subject is a
	// local repository that agents read directly.
	ModeExplainer Mode = "explainer"
	// ModeWhitepaper produces a detailed paper in the form of an academic or
	// industry paper: abstract, numbered sections, figures and tables with
	// captions, bracketed citations and a reference list. Deep research, and
	// the reference list is always included.
	ModeWhitepaper Mode = "whitepaper"
)

// Providers are the agent runtimes Boku can drive.
const (
	ProviderClaudeCode = "claude-code"
	ProviderOllama     = "ollama"
	ProviderOpenAI     = "openai" // any OpenAI-compatible chat completions endpoint
)

type Config struct {
	Project  Project  `yaml:"project" json:"project"`
	Agents   Agents   `yaml:"agents" json:"agents"`
	Local    Local    `yaml:"local" json:"local"`
	Search   Search   `yaml:"search" json:"search"`
	Research Research `yaml:"research" json:"research"`
	Report   Report   `yaml:"report" json:"report"`
	Output   Output   `yaml:"output" json:"output"`
}

type Project struct {
	Name string `yaml:"name" json:"name"`
}

type Agents struct {
	// Provider selects the agent runtime: claude-code (default), ollama, or
	// openai (any OpenAI-compatible endpoint: vLLM, LM Studio, OpenRouter, …).
	Provider string `yaml:"provider" json:"provider"`
	// Command is the Claude Code executable.
	Command string `yaml:"command" json:"command"`
	// Endpoint is the base URL for ollama/openai providers, e.g.
	// http://localhost:11434 or http://localhost:8000/v1.
	Endpoint string `yaml:"endpoint" json:"endpoint,omitempty"`
	// APIKeyEnv names the environment variable holding the API key (openai).
	APIKeyEnv string `yaml:"api_key_env" json:"api_key_env,omitempty"`
	// ContextTokens is the context window requested from ollama (num_ctx).
	ContextTokens int `yaml:"context_tokens" json:"context_tokens,omitempty"`
	// PriceInputPerMTok and PriceOutputPerMTok let Boku compute cost for
	// ollama/openai providers (USD per million tokens). 0 = free.
	PriceInputPerMTok  float64 `yaml:"price_input_per_mtok" json:"price_input_per_mtok,omitempty"`
	PriceOutputPerMTok float64 `yaml:"price_output_per_mtok" json:"price_output_per_mtok,omitempty"`
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

// Local configures a small local model (Ollama) that takes work off the main
// provider: formatting passes always (when reachable), and every role in
// quick mode.
type Local struct {
	// Endpoint is the Ollama server.
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	// Model is the Ollama model tag, e.g. llama3.1:8b or qwen2.5:7b.
	Model string `yaml:"model" json:"model"`
	// ContextTokens is the context window requested (num_ctx).
	ContextTokens int `yaml:"context_tokens" json:"context_tokens"`
	// Format enables the local formatting pass that fixes style problems in
	// the editorial draft without another call to the main provider.
	Format bool `yaml:"format" json:"format"`
	// Roles are routed to the local model instead of agents.provider.
	// Quick mode routes every role; "*" means all roles.
	Roles []string `yaml:"roles" json:"roles,omitempty"`
}

// Search configures Boku's own web retrieval, used by providers without
// built-in web tools (ollama, openai). Claude Code searches by itself.
type Search struct {
	// Engine is duckduckgo (no key needed) or searxng.
	Engine string `yaml:"engine" json:"engine"`
	// SearxngURL is the base URL of a SearXNG instance with JSON output enabled.
	SearxngURL string `yaml:"searxng_url" json:"searxng_url,omitempty"`
	// ResultsPerQuery is how many results are taken from each search.
	ResultsPerQuery int `yaml:"results_per_query" json:"results_per_query"`
	// MaxPages bounds the pages fetched for one agent task.
	MaxPages int `yaml:"max_pages" json:"max_pages"`
	// PageChars bounds the text kept from each page.
	PageChars int `yaml:"page_chars" json:"page_chars"`
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
	// FactCheck runs the independent fact-checking agent. Quick mode turns it off.
	FactCheck bool `yaml:"fact_check" json:"fact_check"`
	// Codebase is the absolute path of a local repository to explain
	// (explainer mode). Agents read it with read-only file tools and cite
	// repository-relative file paths as sources.
	Codebase string `yaml:"codebase" json:"codebase,omitempty"`
}

type Report struct {
	// Mode is full (default), short, quick, explainer or whitepaper; see Mode.
	Mode Mode `yaml:"mode" json:"mode"`
	// Layout is auto, full (cover + contents), compact (title block, no
	// cover) or paper (academic paper; the whitepaper default). auto lets the
	// editor choose between full and compact from the request and the mode.
	Layout string `yaml:"layout" json:"layout"`
	// IncludeReferences puts the source list and evidence register in the
	// PDF/HTML/Markdown. They are always written to <report>.references.json.
	IncludeReferences bool `yaml:"include_references" json:"include_references"`
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
		Local: Local{
			Endpoint:      "http://localhost:11434",
			Model:         "llama3.1:8b",
			ContextTokens: 16384,
			Format:        true,
		},
		Search: Search{
			Engine:          "duckduckgo",
			ResultsPerQuery: 5,
			MaxPages:        10,
			PageChars:       4000,
		},
		Research: Research{
			Depth:         DepthStandard,
			FreshnessDays: 365,
			MaxIterations: 2,
			FactCheck:     true,
		},
		Report: Report{
			Mode:      ModeFull,
			Layout:    "auto",
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
	switch c.Agents.Provider {
	case ProviderClaudeCode:
	case ProviderOllama, ProviderOpenAI:
		if c.Agents.Model == "" {
			errs = append(errs, fmt.Errorf("agents.model is required for provider %s", c.Agents.Provider))
		}
		if c.Agents.Provider == ProviderOpenAI && c.Agents.Endpoint == "" {
			errs = append(errs, errors.New("agents.endpoint is required for provider openai (e.g. http://localhost:8000/v1)"))
		}
	default:
		errs = append(errs, fmt.Errorf("agents.provider %q is not supported (supported: claude-code, ollama, openai)", c.Agents.Provider))
	}
	switch c.Report.Mode {
	case ModeFull, ModeShort, ModeQuick, ModeExplainer, ModeWhitepaper:
	default:
		errs = append(errs, fmt.Errorf("report.mode %q must be full, short, quick, explainer or whitepaper", c.Report.Mode))
	}
	if c.Research.Codebase != "" {
		// Only Claude Code has file tools; other providers would have to guess.
		if c.Agents.Provider != ProviderClaudeCode || c.UsesLocal() {
			errs = append(errs, errors.New("explaining a codebase needs agents.provider claude-code and no local roles (not --quick)"))
		}
		if c.Report.Mode != ModeExplainer {
			errs = append(errs, errors.New("research.codebase is only used in explainer mode"))
		}
	}
	switch c.Report.Layout {
	case "auto", "full", "compact", "paper":
	default:
		errs = append(errs, fmt.Errorf("report.layout %q must be auto, full, compact or paper", c.Report.Layout))
	}
	switch c.Search.Engine {
	case "duckduckgo":
	case "searxng":
		if c.Search.SearxngURL == "" {
			errs = append(errs, errors.New("search.searxng_url is required for search.engine searxng"))
		}
	default:
		errs = append(errs, fmt.Errorf("search.engine %q must be duckduckgo or searxng", c.Search.Engine))
	}
	if c.Search.MaxPages < 1 || c.Search.ResultsPerQuery < 1 || c.Search.PageChars < 500 {
		errs = append(errs, errors.New("search.max_pages and search.results_per_query must be >= 1, search.page_chars >= 500"))
	}
	if c.UsesLocal() && c.Local.Model == "" {
		errs = append(errs, errors.New("local.model is required when local roles or quick mode are used"))
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
	if c.Report.Mode == ModeExplainer {
		return 8 // an explainer rests on a few good sources (or files), not a survey
	}
	switch c.Research.Depth {
	case DepthQuick:
		return 8
	case DepthDeep:
		return 35
	default:
		return 20
	}
}

// ApplyMode adjusts the configuration for a report mode. Call it before
// applying explicit CLI flags so the flags still win.
func (c *Config) ApplyMode(m Mode) {
	c.Report.Mode = m
	switch m {
	case ModeShort:
		c.Research.Depth = DepthQuick
		c.Agents.MaxAgents = min(c.Agents.MaxAgents, 3)
		c.Research.MaxIterations = 0
		if c.Report.Layout == "auto" {
			c.Report.Layout = "compact"
		}
	case ModeWhitepaper:
		c.Research.Depth = DepthDeep
		c.Report.IncludeReferences = true // a paper without its references is not a paper
		c.Report.Charts, c.Report.Diagrams = true, true
		if c.Report.Layout == "auto" {
			c.Report.Layout = "paper"
		}
	case ModeExplainer:
		c.Agents.MaxAgents = min(c.Agents.MaxAgents, 4)
		c.Research.MaxIterations = min(c.Research.MaxIterations, 1)
		c.Report.Diagrams = true
	case ModeQuick:
		c.Research.Depth = DepthQuick
		c.Agents.MaxAgents = min(c.Agents.MaxAgents, 3)
		c.Agents.MaxParallel = min(c.Agents.MaxParallel, 2)
		c.Research.MaxIterations = 1
		c.Research.FactCheck = false
		c.Local.Roles = []string{"*"}
		if c.Report.Layout == "auto" {
			c.Report.Layout = "compact"
		}
	}
}

// IsLocalRole reports whether role runs on the local model.
func (c Config) IsLocalRole(role string) bool {
	for _, r := range c.Local.Roles {
		if r == "*" || r == role {
			return true
		}
	}
	return false
}

// UsesLocal reports whether any role is routed to the local model.
func (c Config) UsesLocal() bool { return len(c.Local.Roles) > 0 }

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
