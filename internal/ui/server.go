// Package ui serves Boku's local web interface: a form over every setting,
// live run progress, and a browser for run artifacts and published reports.
//
// The server is a thin layer over the same pieces the CLI uses. Runs are
// ordinary run directories, so a run started here can be resumed with
// `boku resume` and a run started from the CLI shows up here.
package ui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/riteshsonawane1372/boku/internal/agent"
	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/logx"
	"github.com/riteshsonawane1372/boku/internal/orchestrator"
	"github.com/riteshsonawane1372/boku/internal/run"
)

//go:embed web
var webFS embed.FS

// Check is one line of `boku doctor`.
type Check struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Optional checks do not fail the doctor when they are not OK.
	Optional bool   `json:"optional"`
	Detail   string `json:"detail"`
}

// Server is the web interface. Set the exported fields, then serve Handler.
type Server struct {
	Version string
	// ConfigPath is the boku.yaml the settings page saves; "" means ./boku.yaml.
	ConfigPath string
	// Token, when set, must accompany every request (cookie or ?token=). It
	// is required when listening beyond loopback.
	Token string
	// Load returns the base configuration (defaults plus boku.yaml).
	Load func() (config.Config, error)
	// Execute runs or, with renderOnly, re-renders a run.
	Execute func(ctx context.Context, cfg config.Config, r *run.Run, log *logx.Logger, renderOnly bool) error
	// Doctor checks the runtime; cfgErr is the error from loading cfg.
	Doctor func(cfg config.Config, cfgErr error) []Check

	base context.Context
	mu   sync.Mutex
	jobs map[string]*job // run ID → the execution in this process
	wg   sync.WaitGroup
}

type job struct {
	dir    string
	action string
	cancel context.CancelFunc
}

// NewToken returns a random access token.
func NewToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Handler returns the HTTP handler. Runs started through it are cancelled
// when ctx is.
func (s *Server) Handler(ctx context.Context) http.Handler {
	s.base = ctx
	s.jobs = map[string]*job{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/doctor", s.handleDoctor)
	mux.HandleFunc("POST /api/resolve", s.handleResolve)
	mux.HandleFunc("PUT /api/config", s.handleSaveConfig)
	mux.HandleFunc("GET /api/runs", s.handleRuns)
	mux.HandleFunc("POST /api/runs", s.handleCreate)
	mux.HandleFunc("GET /api/runs/{id}", s.handleRun)
	mux.HandleFunc("DELETE /api/runs/{id}", s.handleDelete)
	mux.HandleFunc("GET /api/runs/{id}/events", s.handleEvents)
	mux.HandleFunc("GET /api/runs/{id}/files", s.handleFiles)
	mux.HandleFunc("GET /api/runs/{id}/file", s.handleFile)
	mux.HandleFunc("GET /api/runs/{id}/output/{format}", s.handleOutput)
	mux.HandleFunc("POST /api/runs/{id}/{action}", s.handleAction)
	mux.HandleFunc("GET /api/reports", s.handleReports)
	mux.HandleFunc("GET /api/reports/file", s.handleReportFile)
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(static)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
	return s.guard(mux)
}

// Wait blocks until every run started by this server has stopped.
func (s *Server) Wait() { s.wg.Wait() }

// guard keeps other websites and other machines out. The server starts
// agents and reads files, so a page in the user's browser must not be able
// to drive it: requests must address a loopback host (which defeats DNS
// rebinding) or carry the token, and anything that changes state needs a
// header a cross-site form or script cannot send without a CORS preflight.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" {
			if q := r.URL.Query().Get("token"); q != "" && r.Method == http.MethodGet && s.tokenOK(q) {
				http.SetCookie(w, &http.Cookie{Name: "boku_token", Value: q, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
				http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
				return
			}
			c, err := r.Cookie("boku_token")
			if err != nil || !s.tokenOK(c.Value) {
				http.Error(w, "access token required: open the URL printed by `boku ui`", http.StatusUnauthorized)
				return
			}
		} else if !loopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Boku") == "" {
			http.Error(w, "missing X-Boku header", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) tokenOK(v string) bool {
	return subtle.ConstantTimeCompare([]byte(v), []byte(s.Token)) == 1
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail reports err; a joined error becomes one message per line.
func fail(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error(), "errors": errorList(err)})
}

func errorList(err error) []string {
	if err == nil {
		return []string{}
	}
	var out []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func readBody(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	return nil
}

// ---------- configuration ----------

func (s *Server) configPath() string {
	if s.ConfigPath != "" {
		return s.ConfigPath
	}
	return "boku.yaml"
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.Load()
	cwd, _ := os.Getwd()
	path := s.configPath()
	_, statErr := os.Stat(path)
	meta := map[string]any{
		"version":        s.Version,
		"cwd":            cwd,
		"config_path":    path,
		"config_exists":  statErr == nil,
		"defaults":       config.Default(),
		"base":           cfg,
		"roles":          agent.AllRoles,
		"research_roles": agent.ResearchRoles,
		"stages":         run.Stages,
	}
	if err != nil {
		meta["config_error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleDoctor(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.Load()
	writeJSON(w, http.StatusOK, s.Doctor(cfg, err))
}

// request describes a report the way the composer does: a mode, then only
// the settings the user changed.
type request struct {
	Topic    string `json:"topic"`
	Mode     string `json:"mode"`
	Codebase string `json:"codebase"`
	Focus    string `json:"focus"`
	// Overrides is a partial configuration document laid over the base
	// configuration after the mode's adjustments, like CLI flags.
	Overrides json.RawMessage `json:"overrides"`
}

// resolve turns a request into the configuration and topic a run would use.
// The configuration is returned even when it does not validate.
func (s *Server) resolve(req request) (config.Config, string, error) {
	cfg, err := s.Load()
	if err != nil {
		return cfg, "", err
	}
	runsDir := cfg.Output.RunsDir
	if req.Mode != "" {
		cfg.ApplyMode(config.Mode(req.Mode))
	}
	if len(req.Overrides) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(req.Overrides)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return cfg, "", fmt.Errorf("invalid settings: %w", err)
		}
	}
	if req.Mode != "" {
		cfg.Report.Mode = config.Mode(req.Mode)
	}
	// The run list reads one runs directory; keep every run in it.
	cfg.Output.RunsDir = runsDir
	cfg.Research.Codebase = ""
	topic := strings.TrimSpace(req.Topic)
	var errs []error
	if dir := strings.TrimSpace(req.Codebase); dir != "" {
		abs, err := filepath.Abs(expandHome(dir))
		if info, statErr := os.Stat(abs); err != nil || statErr != nil || !info.IsDir() {
			errs = append(errs, fmt.Errorf("codebase %q is not a directory", dir))
		} else {
			cfg.Research.Codebase = abs
			topic = orchestrator.CodebaseTopic(abs, req.Focus)
		}
	}
	errs = append(errs, cfg.Validate())
	return cfg, topic, errors.Join(errs...)
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	cfg, topic, err := s.resolve(req)
	writeJSON(w, http.StatusOK, map[string]any{
		"config": cfg, "topic": topic, "errors": errorList(err), "min_sources": cfg.MinSourcesFor(),
	})
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config json.RawMessage `json:"config"`
	}
	if err := readBody(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	cfg := config.Default()
	dec := json.NewDecoder(strings.NewReader(string(body.Config)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("invalid settings: %w", err))
		return
	}
	cfg.Research.Codebase = "" // chosen per run, never a default
	if err := cfg.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	path := s.configPath()
	b = append([]byte("# Boku configuration, saved from the web UI. See `boku init` for a commented example.\n"), b...)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

// ---------- runs ----------

// runView is a manifest plus what only the server knows about the run.
type runView struct {
	run.Manifest
	Dir string `json:"dir"`
	// State is the manifest status, except that a run which was cancelled, or
	// is marked running while this server is not executing it, is
	// "interrupted": it was stopped, or the CLI is running it in another
	// terminal.
	State string `json:"state"`
	// Active is true while this server is executing the run.
	Active bool   `json:"active"`
	Action string `json:"action,omitempty"`
}

func validID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`) && filepath.IsLocal(id)
}

// runDir locates a run: where this server started it, else under the
// configured runs directory.
func (s *Server) runDir(id string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("invalid run id %q", id)
	}
	s.mu.Lock()
	j := s.jobs[id]
	s.mu.Unlock()
	if j != nil {
		return j.dir, nil
	}
	cfg, _ := s.Load()
	return filepath.Join(cfg.Output.RunsDir, id), nil
}

func (s *Server) view(dir string) (runView, error) {
	r, err := run.Open(dir)
	if err != nil {
		return runView{}, err
	}
	v := runView{Manifest: r.Manifest(), Dir: dir}
	s.mu.Lock()
	j := s.jobs[v.ID]
	s.mu.Unlock()
	v.State = v.Status
	if j != nil {
		v.Active, v.Action, v.State = true, j.action, run.StatusRunning
	} else if v.Status == run.StatusRunning || strings.Contains(v.Error, context.Canceled.Error()) {
		v.State = "interrupted"
	}
	return v, nil
}

func (s *Server) handleRuns(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := s.Load()
	entries, _ := os.ReadDir(cfg.Output.RunsDir)
	runs := []runView{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := s.view(filepath.Join(cfg.Output.RunsDir, e.Name()))
		if err != nil {
			continue
		}
		v.Prompts = nil
		runs = append(runs, v)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "runs_dir": cfg.Output.RunsDir})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	dir, err := s.runDir(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	v, err := s.view(dir)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	cfg, topic, err := s.resolve(req)
	if err == nil && topic == "" {
		err = errors.New("a research topic is required")
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	rn, err := run.Create(cfg.Output.RunsDir, topic, cfg, time.Now())
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	id := rn.Manifest().ID
	if err := s.start(id, rn, cfg, "run"); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
}

// start executes a run in the background and records it as active.
func (s *Server) start(id string, rn *run.Run, cfg config.Config, action string) error {
	ctx, cancel := context.WithCancel(s.base)
	s.mu.Lock()
	if s.jobs[id] != nil {
		s.mu.Unlock()
		cancel()
		return errors.New("this run is already in progress")
	}
	s.jobs[id] = &job{dir: rn.Dir, action: action, cancel: cancel}
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		// The log file is the only sink: the events stream tails it, so the
		// UI shows runs started from the CLI the same way.
		log := logx.New(io.Discard, false)
		_ = os.MkdirAll(rn.Path("logs"), 0o755)
		f, ferr := os.OpenFile(rn.Path("logs", "boku.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if ferr == nil {
			defer f.Close()
			log.AttachFile(f)
		}
		log.Info("started from the web UI", "action", action)
		err := s.Execute(ctx, cfg, rn, log, action == "render")
		if ferr == nil {
			log.AttachFile(f) // Execute detaches the file when it returns
		}
		switch {
		case err == nil:
			log.Info("finished", "cost", fmt.Sprintf("$%.2f", rn.Manifest().CostUSD))
		case errors.Is(err, context.Canceled):
			log.Warn("cancelled; resume to continue from the last finished step")
		default:
			log.Error(err.Error())
		}
		if err != nil && action != "render" {
			// Failures before the pipeline starts (no Chrome, model
			// unreachable) leave the manifest saying "running".
			_ = rn.Update(func(m *run.Manifest) {
				if m.Status == run.StatusRunning {
					m.Status, m.Error = run.StatusFailed, err.Error()
				}
			})
		}
		s.mu.Lock()
		delete(s.jobs, id)
		s.mu.Unlock()
	}()
	return nil
}

// reopen holds the settings that may change when a run is resumed or
// re-rendered, the same ones `boku resume` accepts as flags.
type reopen struct {
	Formats           []string `json:"formats"`
	Output            string   `json:"output"`
	MaxCostUSD        *float64 `json:"max_cost_usd"`
	MaxParallel       *int     `json:"max_parallel"`
	IncludeReferences *bool    `json:"include_references"`
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	dir, err := s.runDir(id)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	switch action {
	case "cancel":
		s.mu.Lock()
		j := s.jobs[id]
		s.mu.Unlock()
		if j == nil {
			fail(w, http.StatusConflict, errors.New("this run is not in progress here"))
			return
		}
		j.cancel()
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
		return
	case "resume", "render":
	default:
		fail(w, http.StatusNotFound, fmt.Errorf("unknown action %q", action))
		return
	}
	var o reopen
	if err := readBody(r, &o); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	rn, err := run.Open(dir)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	cfg := rn.Manifest().Config
	if len(o.Formats) > 0 {
		cfg.Report.Formats = o.Formats
	}
	if o.Output != "" {
		cfg.Output.Directory = o.Output
	}
	if o.MaxCostUSD != nil {
		cfg.Agents.MaxCostUSD = *o.MaxCostUSD
	}
	if o.MaxParallel != nil {
		cfg.Agents.MaxParallel = *o.MaxParallel
	}
	if o.IncludeReferences != nil {
		cfg.Report.IncludeReferences = *o.IncludeReferences
	}
	if err := cfg.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.start(id, rn, cfg, action); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	_ = rn.Update(func(m *run.Manifest) { m.Config = cfg })
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir, err := s.runDir(id)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	active := s.jobs[id] != nil
	s.mu.Unlock()
	if active {
		fail(w, http.StatusConflict, errors.New("cancel the run before deleting it"))
		return
	}
	if _, err := run.Open(dir); err != nil { // only ever remove a run directory
		fail(w, http.StatusNotFound, err)
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

// handleEvents streams a run as server-sent events: "run" whenever the
// manifest changes and "log" with new lines of the run log.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	dir, err := s.runDir(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	}
	var (
		lastRun string
		offset  int64
		partial string
		beat    int
	)
	tick := time.NewTicker(600 * time.Millisecond)
	defer tick.Stop()
	for {
		if v, err := s.view(dir); err == nil {
			if b, _ := json.Marshal(v); string(b) != lastRun {
				lastRun = string(b)
				fmt.Fprintf(w, "event: run\ndata: %s\n\n", b)
			}
		}
		if lines := tail(filepath.Join(dir, "logs", "boku.log"), &offset, &partial); len(lines) > 0 {
			send("log", lines)
		}
		if beat++; beat%25 == 0 {
			fmt.Fprint(w, ": keep-alive\n\n")
		}
		fl.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-s.base.Done():
			return
		case <-tick.C:
		}
	}
}

// tail returns the complete lines appended to path since *offset, keeping an
// unfinished last line in *partial for the next call.
func tail(path string, offset *int64, partial *string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == *offset {
		return nil
	}
	if info.Size() < *offset { // truncated or replaced
		*offset, *partial = 0, ""
	}
	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(f, 4<<20))
	*offset += int64(len(b))
	text := *partial + string(b)
	i := strings.LastIndexByte(text, '\n')
	if i < 0 {
		*partial = text
		return nil
	}
	*partial = text[i+1:]
	return strings.Split(text[:i], "\n")
}

// ---------- files ----------

type fileInfo struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Run      string    `json:"run,omitempty"`
}

const maxListed = 3000

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	dir, err := s.runDir(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	files := []fileInfo{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if len(files) >= maxListed {
			return filepath.SkipAll
		}
		if info, err := d.Info(); err == nil {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, fileInfo{Path: filepath.ToSlash(rel), Size: info.Size(), Modified: info.ModTime()})
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "truncated": len(files) >= maxListed})
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	dir, err := s.runDir(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	rel := filepath.FromSlash(r.URL.Query().Get("path"))
	if !filepath.IsLocal(rel) {
		fail(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	serveFile(w, r, filepath.Join(dir, rel))
}

func (s *Server) handleOutput(w http.ResponseWriter, r *http.Request) {
	dir, err := s.runDir(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	rn, err := run.Open(dir)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	p, ok := rn.Manifest().Outputs[r.PathValue("format")]
	if !ok {
		fail(w, http.StatusNotFound, errors.New("this run has no such output"))
		return
	}
	serveFile(w, r, p)
}

func (s *Server) handleReports(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := s.Load()
	// Which run published which file, so a report links back to its run.
	owner := map[string]string{}
	if entries, err := os.ReadDir(cfg.Output.RunsDir); err == nil {
		for _, e := range entries {
			if rn, err := run.Open(filepath.Join(cfg.Output.RunsDir, e.Name())); err == nil {
				m := rn.Manifest()
				for _, p := range m.Outputs {
					if abs, err := filepath.Abs(p); err == nil {
						owner[abs] = m.ID
					}
				}
			}
		}
	}
	files := []fileInfo{}
	entries, _ := os.ReadDir(cfg.Output.Directory)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		abs, _ := filepath.Abs(filepath.Join(cfg.Output.Directory, e.Name()))
		files = append(files, fileInfo{Path: e.Name(), Size: info.Size(), Modified: info.ModTime(), Run: owner[abs]})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Modified.After(files[j].Modified) })
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "directory": cfg.Output.Directory})
}

func (s *Server) handleReportFile(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !validID(name) {
		fail(w, http.StatusBadRequest, errors.New("invalid name"))
		return
	}
	cfg, _ := s.Load()
	serveFile(w, r, filepath.Join(cfg.Output.Directory, name))
}

var contentTypes = map[string]string{
	".json": "application/json; charset=utf-8",
	".md":   "text/plain; charset=utf-8",
	".log":  "text/plain; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".yaml": "text/plain; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".svg":  "image/svg+xml",
	".pdf":  "application/pdf",
	".png":  "image/png",
	".jpg":  "image/jpeg",
}

// serveFile sends a run artifact or report. Reports are built from what
// agents read on the web, so anything a browser would execute is sandboxed
// into its own origin where it cannot reach this API.
func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("file not found"))
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		fail(w, http.StatusNotFound, errors.New("file not found"))
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	ct, known := contentTypes[ext]
	if !known {
		ct = "application/octet-stream"
	}
	if ext == ".html" || ext == ".svg" {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	if !known || r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
