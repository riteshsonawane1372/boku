// Command boku is a multi-agent research and report generator.
//
//	boku report "How Kubernetes is being used for AI infrastructure"
//	boku explain ./path/to/repo "how a request is handled"
//	boku whitepaper "Sparse attention for long-context transformers"
//	boku resume runs/2026-09-23T074500-kubernetes-ai-infrastructure
//	boku status runs/2026-09-23T074500-kubernetes-ai-infrastructure
//	boku ui
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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
	"github.com/riteshsonawane1372/boku/internal/ui"
	"github.com/riteshsonawane1372/boku/prompts"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `Boku — multi-agent research and report generation.

Usage:
  boku report <topic> [flags]     research a topic and publish a report
                                    --short     short search report (a few pages)
                                    --quick     local Ollama model only, no Claude tokens
                                    --save-ref  include the source list in the PDF
  boku research <topic> [flags]   alias for report
  boku explain <topic|path> [focus] [flags]
                                  visual explainer of a topic, or of a local
                                  codebase when given a directory
  boku whitepaper <topic> [flags] detailed paper in academic/industry format:
                                  abstract, numbered sections, references
  boku resume <run-dir> [flags]   continue an interrupted or blocked run
  boku render <run-dir> [flags]   rebuild outputs from a run's artifacts (no agents)
  boku status <run-dir>           show a run's stages, tasks and cost
  boku ui [flags]                 open the web interface: start runs with every
                                  option, watch them live, browse reports
  boku doctor                     check the agent runtime, Ollama and Chrome
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
		err = cmdReport(ctx, "report", args)
	case "explain":
		err = cmdReport(ctx, "explain", args)
	case "whitepaper":
		err = cmdReport(ctx, "whitepaper", args)
	case "resume":
		err = cmdResume(ctx, args)
	case "render":
		err = cmdRender(ctx, args)
	case "status":
		err = cmdStatus(args)
	case "ui", "serve":
		err = cmdUI(ctx, args)
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
	mode       string
	short      bool
	quick      bool
	explainer  bool
	whitepaper bool
	saveRef    bool
	localModel string
}

func (o *options) register(fs *flag.FlagSet, full bool) {
	fs.StringVar(&o.configPath, "config", "", "path to boku.yaml (default: ./boku.yaml if present)")
	fs.StringVar(&o.output, "output", "", "directory for published reports")
	fs.StringVar(&o.format, "format", "", "output formats, comma-separated: pdf,html,md")
	fs.BoolVar(&o.verbose, "verbose", false, "show debug logging")
	fs.Float64Var(&o.maxCost, "max-cost", -1, "stop launching agents once spend reaches this many USD (0 = unlimited)")
	fs.IntVar(&o.parallel, "parallel", 0, "maximum concurrently running agents")
	fs.BoolVar(&o.saveRef, "save-ref", false, "include the numbered source list and evidence register in the report (always written to <report>.references.json)")
	if !full {
		return
	}
	fs.StringVar(&o.mode, "mode", "", "report mode: full (default), short, quick, explainer or whitepaper")
	fs.BoolVar(&o.whitepaper, "whitepaper", false, "detailed whitepaper in academic/industry paper format: abstract, numbered sections, figures, references; deep research (same as --mode whitepaper or `boku whitepaper`)")
	fs.BoolVar(&o.explainer, "explainer", false, "visual explainer of a topic or, given a directory, a codebase (same as --mode explainer or `boku explain`)")
	fs.BoolVar(&o.short, "short", false, "short search report: 3 workstreams, one fact-check round, compact layout (same as --mode short)")
	fs.BoolVar(&o.quick, "quick", false, "quick report on the local Ollama model: no Claude Code tokens, no fact-check (same as --mode quick)")
	fs.StringVar(&o.localModel, "local-model", "", "Ollama model for --quick and formatting, e.g. llama3.1:8b, qwen2.5:14b")
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
	mode := config.Mode(o.mode)
	switch {
	case btoi(o.quick)+btoi(o.short)+btoi(o.explainer)+btoi(o.whitepaper) > 1:
		return errors.New("--quick, --short, --explainer and --whitepaper are mutually exclusive")
	case o.quick:
		mode = config.ModeQuick
	case o.short:
		mode = config.ModeShort
	case o.explainer:
		mode = config.ModeExplainer
	case o.whitepaper:
		mode = config.ModeWhitepaper
	}
	if mode != "" {
		cfg.ApplyMode(mode) // before the other flags, so explicit flags still win
	}
	if o.saveRef {
		cfg.Report.IncludeReferences = true
	}
	if o.localModel != "" {
		cfg.Local.Model = o.localModel
	}
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

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// cmdReport runs `boku report`, `boku explain` and `boku whitepaper`. An explainer whose first
// argument is a directory explains that codebase; the remaining arguments
// say what to focus on.
func cmdReport(ctx context.Context, name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var o options
	o.register(fs, true)
	fs.Usage = func() {
		switch name {
		case "explain":
			fmt.Fprintln(fs.Output(), "Usage: boku explain <topic> [flags]\n       boku explain <directory> [focus] [flags]")
		case "whitepaper":
			fmt.Fprintln(fs.Output(), "Usage: boku whitepaper <topic> [flags]")
		default:
			fmt.Fprintln(fs.Output(), "Usage: boku report <topic> [flags]")
		}
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if forced := map[string]config.Mode{"explain": config.ModeExplainer, "whitepaper": config.ModeWhitepaper}[name]; forced != "" {
		if o.mode != "" && o.mode != string(forced) || btoi(o.quick)+btoi(o.short)+btoi(o.explainer)+btoi(o.whitepaper) > 0 {
			return fmt.Errorf("boku %s always uses %s mode; drop --mode, --short, --quick, --explainer and --whitepaper", name, forced)
		}
		o.mode = string(forced)
	}
	explainer := o.explainer || o.mode == string(config.ModeExplainer)
	var codebase string
	if explainer && len(pos) > 0 {
		if codebase, err = codebasePath(pos[0]); err != nil {
			return err
		}
		if codebase != "" {
			pos = []string{orchestrator.CodebaseTopic(codebase, strings.Join(pos[1:], " "))}
		}
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
	cfg.Research.Codebase = codebase
	if err := o.apply(&cfg); err != nil {
		return err
	}
	r, err := run.Create(cfg.Output.RunsDir, topic, cfg, time.Now())
	if err != nil {
		return err
	}
	return execute(ctx, cfg, r, o.verbose, false)
}

// codebasePath returns the absolute path of arg when it names a directory,
// and "" when it is a topic.
func codebasePath(arg string) (string, error) {
	info, err := os.Stat(arg)
	if err != nil || !info.IsDir() {
		return "", nil
	}
	return filepath.Abs(arg)
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
	return executeWith(ctx, cfg, r, logx.New(os.Stderr, verbose), renderOnly)
}

// executeWith runs or re-renders r, logging to log. The web UI calls it with
// a logger that writes to the run's log file only.
func executeWith(ctx context.Context, cfg config.Config, r *run.Run, log *logx.Logger, renderOnly bool) error {
	lib, err := prompts.Load(cfg.Output.PromptsDir)
	if err != nil {
		return err
	}
	orc := &orchestrator.Orchestrator{
		Config:  cfg,
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
		if orc.Agent, orc.Formatter, err = newAgents(ctx, cfg, log); err != nil {
			return err
		}
		describeRuntime(cfg, orc.Formatter != nil, log)
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

// newAgents returns the agent runtime for the run and, when the local model
// is reachable, the formatter that runs style fixes on it. Add providers here.
func newAgents(ctx context.Context, cfg config.Config, log *logx.Logger) (agent.Agent, agent.Agent, error) {
	timeout := time.Duration(cfg.Agents.Timeout)
	web := &agent.Web{
		Engine: cfg.Search.Engine, SearxngURL: cfg.Search.SearxngURL,
		ResultsPerQuery: cfg.Search.ResultsPerQuery, MaxPages: cfg.Search.MaxPages, PageChars: cfg.Search.PageChars,
	}
	var main agent.Agent
	switch cfg.Agents.Provider {
	case config.ProviderOllama, config.ProviderOpenAI:
		api, endpoint := agent.APIOllama, cfg.Agents.Endpoint
		if cfg.Agents.Provider == config.ProviderOpenAI {
			api = agent.APIOpenAI
		} else if endpoint == "" {
			endpoint = cfg.Local.Endpoint
		}
		var key string
		if cfg.Agents.APIKeyEnv != "" {
			if key = os.Getenv(cfg.Agents.APIKeyEnv); key == "" {
				return nil, nil, fmt.Errorf("agents.api_key_env: %s is not set", cfg.Agents.APIKeyEnv)
			}
		}
		main = &agent.ChatModel{
			API: api, Endpoint: endpoint, Model: cfg.Agents.Model, APIKey: key,
			ContextTokens: nonZero(cfg.Agents.ContextTokens, cfg.Local.ContextTokens), DefaultTimeout: timeout, Web: web,
			PriceInputPerMTok: cfg.Agents.PriceInputPerMTok, PriceOutputPerMTok: cfg.Agents.PriceOutputPerMTok,
		}
	default: // "claude-code"; Validate rejects unknown providers
		main = &agent.ClaudeCode{
			Command:        cfg.Agents.Command,
			DefaultTimeout: timeout,
			EnvPassthrough: cfg.Agents.EnvPassthrough,
		}
	}

	local := &agent.ChatModel{
		API: agent.APIOllama, Endpoint: cfg.Local.Endpoint, Model: cfg.Local.Model,
		ContextTokens: cfg.Local.ContextTokens, DefaultTimeout: timeout, Web: web,
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	pingErr := agent.Ping(pingCtx, cfg.Local.Endpoint, cfg.Local.Model)
	cancel()
	if cfg.UsesLocal() {
		if pingErr != nil {
			return nil, nil, fmt.Errorf("%s mode needs the local model: %w (install https://ollama.com, then `ollama pull %s`, or pass --local-model)", cfg.Report.Mode, pingErr, cfg.Local.Model)
		}
		roles := map[string]agent.Agent{}
		for _, role := range agent.AllRoles {
			if cfg.IsLocalRole(role) {
				roles[role] = local
			}
		}
		main = &agent.Router{Default: main, Roles: roles}
	}
	var formatter agent.Agent
	if cfg.Local.Format {
		if pingErr == nil {
			formatter = local
		} else {
			log.Debug("local formatter unavailable", "reason", pingErr.Error())
		}
	}
	return main, formatter, nil
}

// describeRuntime prints which models will do the work.
func describeRuntime(cfg config.Config, formatter bool, log *logx.Logger) {
	main := cfg.Agents.Provider
	if cfg.Agents.Model != "" {
		main += " (" + cfg.Agents.Model + ")"
	}
	switch {
	case cfg.Report.Mode == config.ModeQuick:
		log.Info("quick report on the local model", "model", cfg.Local.Model, "fact_check", "off")
	case cfg.UsesLocal():
		log.Info("agent runtime", "main", main, "local", cfg.Local.Model, "local_roles", strings.Join(cfg.Local.Roles, ","))
	default:
		log.Info("agent runtime", "main", main, "mode", cfg.Report.Mode)
	}
	if formatter {
		log.Debug("local formatter enabled", "model", cfg.Local.Model)
	}
}

func nonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
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
	cfg, err := loadConfig("")
	ok := true
	for _, c := range doctorChecks(cfg, err) {
		switch {
		case c.OK:
			fmt.Printf("  ✓ %-12s %s\n", c.Name, c.Detail)
		case c.Optional:
			fmt.Printf("  - %-12s %s\n", c.Name, c.Detail)
		default:
			ok = false
			fmt.Printf("  ✗ %-12s %s\n", c.Name, c.Detail)
		}
	}
	if !ok {
		return errors.New("some checks failed")
	}
	return nil
}

// doctorChecks inspects everything a run depends on; `boku doctor` prints
// the result and the web UI shows it.
func doctorChecks(cfg config.Config, cfgErr error) []ui.Check {
	var checks []ui.Check
	check := func(name string, err error, detail string) {
		if err != nil {
			checks = append(checks, ui.Check{Name: name, Detail: err.Error()})
			return
		}
		checks = append(checks, ui.Check{Name: name, OK: true, Detail: detail})
	}
	check("config", cfgErr, "ok")
	switch cfg.Agents.Provider {
	case config.ProviderClaudeCode:
		if p, err := exec.LookPath(cfg.Agents.Command); err != nil {
			check("claude code", err, "")
		} else {
			v, _ := exec.Command(p, "--version").Output()
			check("claude code", nil, strings.TrimSpace(string(v))+" ("+p+")")
		}
	default:
		check("provider", nil, fmt.Sprintf("%s %s at %s", cfg.Agents.Provider, cfg.Agents.Model, cfg.Agents.Endpoint))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := agent.Ping(ctx, cfg.Local.Endpoint, cfg.Local.Model); err != nil {
		if cfg.UsesLocal() {
			check("ollama", err, "")
		} else {
			checks = append(checks, ui.Check{Name: "ollama", Optional: true, Detail: "not available (" + err.Error() + "); --quick and local formatting disabled"})
		}
	} else {
		check("ollama", nil, cfg.Local.Model+" at "+cfg.Local.Endpoint+" (--quick and local formatting available)")
	}
	p, err := render.FindChrome(cfg.Report.Chrome)
	check("chrome", err, p)
	_, err = prompts.Load(cfg.Output.PromptsDir)
	check("prompts", err, "ok")
	return checks
}

// cmdUI serves the web interface until interrupted. Runs it started are
// cancelled on exit and can be resumed like any other.
func cmdUI(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "address to listen on; anything but loopback requires the access token printed at start")
	port := fs.Int("port", 7878, "port to listen on")
	configPath := fs.String("config", "", "path to boku.yaml (default: ./boku.yaml if present)")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	srv := &ui.Server{
		Version:    version,
		ConfigPath: *configPath,
		Load:       func() (config.Config, error) { return loadConfig(*configPath) },
		Execute:    executeWith,
		Doctor:     doctorChecks,
	}
	if ip := net.ParseIP(*host); *host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		token, err := ui.NewToken()
		if err != nil {
			return err
		}
		srv.Token = token
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(*host, fmt.Sprint(*port)))
	if err != nil {
		return fmt.Errorf("%w (is another `boku ui` running? try --port)", err)
	}
	url := "http://" + ln.Addr().String() + "/"
	if srv.Token != "" {
		url += "?token=" + srv.Token
		fmt.Fprintln(os.Stderr, "WARN  listening beyond loopback: anyone with this URL can start agents and read run files")
	}
	fmt.Printf("Boku UI  %s\nCtrl-C to stop.\n", url)
	if !*noOpen {
		openBrowser(url)
	}
	hs := &http.Server{Handler: srv.Handler(ctx), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = hs.Shutdown(shut)
		_ = hs.Close() // event streams never go idle
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	srv.Wait()
	return nil
}

func openBrowser(url string) {
	name := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name = "explorer"
	}
	_ = exec.Command(name, url).Start()
}

const exampleConfig = `# Boku configuration. Every field is optional; these are the defaults.
project:
  name: boku

agents:
  provider: claude-code      # claude-code | ollama | openai (any OpenAI-compatible API)
  command: claude            # Claude Code executable
  model: ""                  # empty = Claude Code default; e.g. opus, sonnet
  # For ollama/openai providers (see examples/custom-model.yaml):
  # endpoint: http://localhost:8000/v1
  # api_key_env: OPENAI_API_KEY
  # price_input_per_mtok: 0    # USD per million tokens, for cost tracking
  # price_output_per_mtok: 0
  role_models: {}            # per-role override, e.g. {editorial: opus, planner: sonnet}
  max_parallel: 4            # agents running at once
  max_agents: 6              # research workstreams the planner may create
  max_retries: 2             # retries per failed agent call
  timeout: 20m               # per agent call
  max_cost_usd: 0            # stop launching agents at this spend; 0 = unlimited
  env_passthrough: []        # extra env vars agents may see (e.g. AWS_PROFILE for Bedrock)

local:                       # small local model (Ollama)
  endpoint: http://localhost:11434
  model: llama3.1:8b         # used by --quick and the formatting pass
  context_tokens: 16384
  format: true               # fix style problems locally instead of another editorial call
  roles: []                  # roles to run locally, e.g. [formatter, synthesizer]; --quick uses all

search:                      # Boku's own web search, for ollama/openai providers
  engine: duckduckgo         # duckduckgo | searxng
  searxng_url: ""
  results_per_query: 5
  max_pages: 10              # pages fetched per research task
  page_chars: 4000

research:
  depth: standard            # quick | standard | deep
  freshness_days: 365        # window for "current" information
  max_iterations: 2          # fact-check → follow-up research rounds
  min_sources: 0             # 0 = derive from depth (8 / 20 / 35)
  sources: []                # preferred sources or source types
  fact_check: true

report:
  mode: full                 # full | short | quick | explainer | whitepaper
  layout: auto               # auto | full | compact | paper (auto: the editor decides; paper for whitepapers)
  include_references: false  # true = source list in the PDF (--save-ref); always in <report>.references.json
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
