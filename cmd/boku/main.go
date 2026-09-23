// Command boku is a multi-agent research and report generator.
//
//	boku report "How Kubernetes is being used for AI infrastructure"
//	boku resume runs/2026-09-23T074500-kubernetes-ai-infrastructure
//	boku status runs/2026-09-23T074500-kubernetes-ai-infrastructure
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/logx"
	"github.com/riteshsonawane1372/boku/internal/orchestrator"
	"github.com/riteshsonawane1372/boku/internal/render"
	"github.com/riteshsonawane1372/boku/internal/run"
	"github.com/riteshsonawane1372/boku/prompts"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `Boku — multi-agent research and report generation.

Usage:
  boku report <topic> [flags]     research a topic and publish a report
  boku research <topic> [flags]   alias for report
  boku resume <run-dir> [flags]   continue an interrupted or blocked run
  boku render <run-dir> [flags]   rebuild outputs from a run's artifacts (no agents)
  boku status <run-dir>           show a run's stages, tasks and cost
  boku doctor                     check that Claude Code and Chrome are available
  boku init [path]                write an example boku.yaml
  boku version

Run 'boku <command> -h' for flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "report", "research":
		err = cmdReport(ctx, args)
	case "resume":
		err = cmdResume(ctx, args)
	case "render":
		err = cmdRender(ctx, args)
	case "status":
		err = cmdStatus(args)
	case "doctor":
		err = cmdDoctor()
	case "init":
		err = cmdInit(args)
	case "version", "--version", "-v":
		fmt.Println("boku", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		var blocked *orchestrator.ErrBlocked
		switch {
		case errors.As(err, &blocked):
			fmt.Fprintf(os.Stderr, "\n✗ %v\n", err)
			os.Exit(3)
		case errors.Is(err, context.Canceled):
			fmt.Fprintln(os.Stderr, "\n✗ interrupted; resume with: boku resume <run-dir>")
			os.Exit(130)
		default:
			fmt.Fprintf(os.Stderr, "\n✗ %v\n", err)
			os.Exit(1)
		}
	}
}

// options are the flags shared by report/resume/render.
type options struct {
	configPath string
	output     string
	runsDir    string
	format     string
	depth      string
	agents     int
	parallel   int
	maxCost    float64
	freshness  string
	sources    string
	model      string
	iterations int
	minSources int
	verbose    bool
}

func (o *options) register(fs *flag.FlagSet, full bool) {
	fs.StringVar(&o.configPath, "config", "", "path to boku.yaml (default: ./boku.yaml if present)")
	fs.StringVar(&o.output, "output", "", "directory for published reports")
	fs.StringVar(&o.format, "format", "", "output formats, comma-separated: pdf,html,md")
	fs.BoolVar(&o.verbose, "verbose", false, "show debug logging")
	fs.Float64Var(&o.maxCost, "max-cost", -1, "stop launching agents once spend reaches this many USD (0 = unlimited)")
	fs.IntVar(&o.parallel, "parallel", 0, "maximum concurrently running agents")
	if !full {
		return
	}
	fs.StringVar(&o.runsDir, "runs", "", "directory for run artifacts")
	fs.StringVar(&o.depth, "depth", "", "research depth: quick, standard or deep")
	fs.IntVar(&o.agents, "agents", 0, "maximum research workstreams (agents) the planner may create")
	fs.StringVar(&o.freshness, "freshness", "", "window for current information, e.g. 30d, 6m, 1y")
	fs.StringVar(&o.sources, "sources", "", "preferred sources or source types, comma-separated")
	fs.StringVar(&o.model, "model", "", "model for all agents (e.g. opus, sonnet)")
	fs.IntVar(&o.iterations, "iterations", -1, "maximum fact-check follow-up rounds")
	fs.IntVar(&o.minSources, "min-sources", 0, "distinct sources the research gate expects")
}

func (o *options) apply(cfg *config.Config) error {
	if o.output != "" {
		cfg.Output.Directory = o.output
	}
	if o.runsDir != "" {
		cfg.Output.RunsDir = o.runsDir
	}
	if o.format != "" {
		cfg.Report.Formats = splitList(o.format)
	}
	if o.depth != "" {
		cfg.Research.Depth = config.Depth(o.depth)
	}
	if o.agents > 0 {
		cfg.Agents.MaxAgents = o.agents
	}
	if o.parallel > 0 {
		cfg.Agents.MaxParallel = o.parallel
	}
	if o.maxCost >= 0 {
		cfg.Agents.MaxCostUSD = o.maxCost
	}
	if o.freshness != "" {
		days, err := config.ParseFreshness(o.freshness)
		if err != nil {
			return err
		}
		cfg.Research.FreshnessDays = days
	}
	if o.sources != "" {
		cfg.Research.Sources = splitList(o.sources)
	}
	if o.model != "" {
		cfg.Agents.Model = o.model
	}
	if o.iterations >= 0 {
		cfg.Research.MaxIterations = o.iterations
	}
	if o.minSources > 0 {
		cfg.Research.MinSources = o.minSources
	}
	return cfg.Validate()
}

// parseInterleaved parses flags that may appear before or after positional
// arguments ("boku report "topic" --depth deep").
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func loadConfig(path string) (config.Config, error) {
	if path == "" {
		if _, err := os.Stat("boku.yaml"); err == nil {
			path = "boku.yaml"
		}
	}
	return config.Load(path)
}

func cmdReport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	var o options
	o.register(fs, true)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: boku report <topic> [flags]")
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	topic := strings.TrimSpace(strings.Join(pos, " "))
	if topic == "" {
		fs.Usage()
		return errors.New("a research topic is required")
	}
	cfg, err := loadConfig(o.configPath)
	if err != nil {
		return err
	}
	if err := o.apply(&cfg); err != nil {
		return err
	}
	r, err := run.Create(cfg.Output.RunsDir, topic, cfg, time.Now())
	if err != nil {
		return err
	}
	return execute(ctx, cfg, r, o.verbose, false)
}

func cmdResume(ctx context.Context, args []string) error {
	return reopen(ctx, "resume", args, false)
}

func cmdRender(ctx context.Context, args []string) error {
	return reopen(ctx, "render", args, true)
}

// reopen continues a run with its recorded configuration; only operational
// flags (output, format, cost, parallelism) may change.
func reopen(ctx context.Context, name string, args []string, renderOnly bool) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var o options
	o.register(fs, false)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: boku %s <run-dir> [flags]\n", name)
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errors.New("exactly one run directory is required")
	}
	r, err := run.Open(pos[0])
	if err != nil {
		return err
	}
	cfg := r.Manifest().Config
	if err := o.apply(&cfg); err != nil {
		return err
	}
	_ = r.Update(func(m *run.Manifest) { m.Config = cfg })
	return execute(ctx, cfg, r, o.verbose, renderOnly)
}

func execute(ctx context.Context, cfg config.Config, r *run.Run, verbose, renderOnly bool) error {
	log := logx.New(os.Stderr, verbose)
	lib, err := prompts.Load(cfg.Output.PromptsDir)
	if err != nil {
		return err
	}
	orc := &orchestrator.Orchestrator{
		Config:  cfg,
		Agent:   newAgent(cfg),
		Prompts: lib,
		Log:     log,
		Printer: &render.PDFPrinter{Browser: cfg.Report.Chrome},
		Version: version,
	}
	var out *orchestrator.Outcome
	if renderOnly {
		out, err = orc.Render(ctx, r)
	} else {
		if !hasPDFPrinter(cfg) {
			return errors.New("PDF output requested but no Chrome/Chromium found; install one, set report.chrome, or use --format html,md")
		}
		out, err = orc.Execute(ctx, r)
	}
	m := r.Manifest()
	if err != nil {
		log.Plain("\nRun directory: %s  (cost so far $%.2f)", r.Dir, m.CostUSD)
		return err
	}
	log.Plain("\n✓ Report generated\n\nOutput:")
	for _, p := range out.Outputs {
		log.Plain("  %s", p)
	}
	log.Plain("\nRun: %s\nCost: $%.2f", r.Dir, m.CostUSD)
	return nil
}

func hasPDFPrinter(cfg config.Config) bool {
	for _, f := range cfg.Report.Formats {
		if f == "pdf" {
			_, err := render.FindChrome(cfg.Report.Chrome)
			return err == nil
		}
	}
	return true
}

// newAgent returns the configured agent runtime. Add new providers here.
func newAgent(cfg config.Config) agent.Agent {
	switch cfg.Agents.Provider {
	default: // "claude-code"; Validate rejects unknown providers
		return &agent.ClaudeCode{
			Command:        cfg.Agents.Command,
			DefaultTimeout: time.Duration(cfg.Agents.Timeout),
			EnvPassthrough: cfg.Agents.EnvPassthrough,
		}
	}
}

func cmdStatus(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: boku status <run-dir>")
	}
	r, err := run.Open(args[0])
	if err != nil {
		return err
	}
	m := r.Manifest()
	fmt.Printf("%s\n%s\n\nstatus: %s   cost: $%.2f   created: %s\n\n", m.ID, m.Topic, m.Status, m.CostUSD, m.CreatedAt.Format(time.RFC1123))
	marks := map[string]string{run.StatusDone: "✓", run.StatusFailed: "✗", run.StatusBlocked: "✗", run.StatusRunning: "…", run.StatusPending: "-"}
	for _, s := range m.Stages {
		line := fmt.Sprintf("  %-10s %s  %s", s.Name, marks[s.Status], s.Status)
		if s.Detail != "" {
			line += "  · " + s.Detail
		}
		fmt.Println(line)
		if s.Error != "" {
			fmt.Printf("               %s\n", firstLine(s.Error))
		}
	}
	if len(m.Tasks) > 0 {
		fmt.Println("\n  tasks:")
		ids := make([]string, 0, len(m.Tasks))
		for id := range m.Tasks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			t := m.Tasks[id]
			fmt.Printf("    %-22s %-12s %-6s attempts=%d  $%.2f  %s\n", t.ID, t.Role, t.Status, t.Attempts, t.CostUSD, t.Duration)
		}
	}
	if len(m.Outputs) > 0 {
		fmt.Println("\n  outputs:")
		for f, p := range m.Outputs {
			fmt.Printf("    %-4s %s\n", f, p)
		}
	}
	if m.Error != "" {
		fmt.Printf("\n  error: %s\n", m.Error)
	}
	return nil
}

func cmdDoctor() error {
	ok := true
	check := func(name string, err error, detail string) {
		if err != nil {
			ok = false
			fmt.Printf("  ✗ %-12s %v\n", name, err)
			return
		}
		fmt.Printf("  ✓ %-12s %s\n", name, detail)
	}
	cfg, err := loadConfig("")
	check("config", err, "ok")
	if p, err := exec.LookPath(cfg.Agents.Command); err != nil {
		check("claude code", err, "")
	} else {
		v, _ := exec.Command(p, "--version").Output()
		check("claude code", nil, strings.TrimSpace(string(v))+" ("+p+")")
	}
	p, err := render.FindChrome(cfg.Report.Chrome)
	check("chrome", err, p)
	_, err = prompts.Load(cfg.Output.PromptsDir)
	check("prompts", err, "ok")
	if !ok {
		return errors.New("some checks failed")
	}
	return nil
}

const exampleConfig = `# Boku configuration. Every field is optional; these are the defaults.
project:
  name: boku

agents:
  provider: claude-code      # the agent runtime
  command: claude            # Claude Code executable
  model: ""                  # empty = Claude Code default; e.g. opus, sonnet
  role_models: {}            # per-role override, e.g. {editorial: opus, planner: sonnet}
  max_parallel: 4            # agents running at once
  max_agents: 6              # research workstreams the planner may create
  max_retries: 2             # retries per failed agent call
  timeout: 20m               # per agent call
  max_cost_usd: 0            # stop launching agents at this spend; 0 = unlimited
  env_passthrough: []        # extra env vars agents may see (e.g. AWS_PROFILE for Bedrock)

research:
  depth: standard            # quick | standard | deep
  freshness_days: 365        # window for "current" information
  max_iterations: 2          # fact-check → follow-up research rounds
  min_sources: 0             # 0 = derive from depth (5 / 10 / 20)
  sources: []                # preferred sources or source types

report:
  formats: [pdf]             # pdf, html, md
  citations: true
  charts: true
  diagrams: true
  page_size: A4              # A4 | Letter
  chrome: ""                 # Chrome/Chromium path; empty = autodetect
  author: ""                 # shown on the cover

output:
  directory: ./reports
  runs_directory: ./runs
  prompts_directory: ""      # override embedded prompts with files from disk
`

func cmdInit(args []string) error {
	path := "boku.yaml"
	if len(args) > 0 {
		path = args[0]
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.WriteFile(path, []byte(exampleConfig), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
