package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteshsonawane1372/boku/internal/config"
	"github.com/riteshsonawane1372/boku/internal/logx"
	"github.com/riteshsonawane1372/boku/internal/run"
)

// newServer returns a server whose runs and reports live under a temp dir
// and whose Execute completes every run immediately.
func newServer(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	s := &Server{
		Version: "test",
		Load: func() (config.Config, error) {
			cfg := config.Default()
			cfg.Output.RunsDir = filepath.Join(dir, "runs")
			cfg.Output.Directory = filepath.Join(dir, "reports")
			return cfg, nil
		},
		Execute: func(_ context.Context, _ config.Config, r *run.Run, log *logx.Logger, _ bool) error {
			log.Info("fake pipeline ran")
			return r.Update(func(m *run.Manifest) { m.Status = run.StatusCompleted })
		},
		Doctor: func(config.Config, error) []Check { return nil },
	}
	return s, s.Handler(context.Background()), dir
}

func do(h http.Handler, method, url, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1:7878"+url, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			req.Host = hdr[i+1]
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestGuard(t *testing.T) {
	_, h, _ := newServer(t)
	if w := do(h, "GET", "/api/meta", ""); w.Code != http.StatusOK {
		t.Fatalf("loopback GET = %d", w.Code)
	}
	if w := do(h, "GET", "/api/meta", "", "Host", "evil.example"); w.Code != http.StatusForbidden {
		t.Errorf("rebound host = %d, want 403", w.Code)
	}
	if w := do(h, "POST", "/api/resolve", "{}"); w.Code != http.StatusForbidden {
		t.Errorf("POST without X-Boku = %d, want 403", w.Code)
	}
}

func TestGuardToken(t *testing.T) {
	s, _, _ := newServer(t)
	s.Token = "secret"
	h := s.Handler(context.Background())
	if w := do(h, "GET", "/api/meta", "", "Host", "10.0.0.5:7878"); w.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", w.Code)
	}
	w := do(h, "GET", "/?token=secret", "", "Host", "10.0.0.5:7878")
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Set-Cookie"), "boku_token=secret") {
		t.Fatalf("token exchange = %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	if w := do(h, "GET", "/api/meta", "", "Host", "10.0.0.5:7878", "Cookie", "boku_token=secret"); w.Code != http.StatusOK {
		t.Errorf("with cookie = %d, want 200", w.Code)
	}
}

func TestResolveAppliesModeThenOverrides(t *testing.T) {
	_, h, _ := newServer(t)
	w := do(h, "POST", "/api/resolve", `{"mode":"short","overrides":{"research":{"depth":"deep"},"report":{"formats":["html"]}}}`, "X-Boku", "1")
	var out struct {
		Config config.Config `json:"config"`
		Errors []string      `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	c := out.Config
	if c.Report.Mode != config.ModeShort || c.Agents.MaxAgents != 3 || c.Report.Layout != "compact" {
		t.Errorf("mode not applied: %+v", c.Report)
	}
	if c.Research.Depth != config.DepthDeep || len(c.Report.Formats) != 1 || c.Report.Formats[0] != "html" {
		t.Errorf("overrides did not win over the mode: depth=%s formats=%v", c.Research.Depth, c.Report.Formats)
	}
	if len(out.Errors) != 0 {
		t.Errorf("unexpected errors: %v", out.Errors)
	}

	w = do(h, "POST", "/api/resolve", `{"mode":"full","overrides":{"research":{"depth":"bogus"}}}`, "X-Boku", "1")
	if !strings.Contains(w.Body.String(), "research.depth") {
		t.Errorf("invalid depth not reported: %s", w.Body)
	}
}

func TestRunLifecycle(t *testing.T) {
	s, h, dir := newServer(t)
	w := do(h, "POST", "/api/runs", `{"topic":"","mode":"full"}`, "X-Boku", "1")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty topic = %d, want 400", w.Code)
	}
	w = do(h, "POST", "/api/runs", `{"topic":"Raft consensus","mode":"full"}`, "X-Boku", "1")
	if w.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	s.Wait()

	var v runView
	_ = json.Unmarshal(do(h, "GET", "/api/runs/"+created.ID, "").Body.Bytes(), &v)
	if v.State != run.StatusCompleted || v.Active {
		t.Errorf("state = %q active = %v", v.State, v.Active)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "runs", created.ID, "logs", "boku.log"))
	if !strings.Contains(string(log), "fake pipeline ran") || !strings.Contains(string(log), "finished") {
		t.Errorf("run log missing lines:\n%s", log)
	}

	for _, p := range []string{"../../etc/passwd", "/etc/passwd", "..%2F..%2Fgo.mod"} {
		if w := do(h, "GET", "/api/runs/"+created.ID+"/file?path="+p, ""); w.Code == http.StatusOK {
			t.Errorf("path %q escaped the run directory", p)
		}
	}
	if w := do(h, "GET", "/api/runs/"+created.ID+"/file?path=manifest.json", ""); w.Code != http.StatusOK {
		t.Errorf("manifest.json = %d", w.Code)
	}

	if w := do(h, "DELETE", "/api/runs/"+created.ID, "", "X-Boku", "1"); w.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", created.ID)); !os.IsNotExist(err) {
		t.Error("run directory still exists")
	}
	// A directory that is not a run is never removed.
	other := filepath.Join(dir, "runs", "not-a-run")
	_ = os.MkdirAll(other, 0o755)
	if w := do(h, "DELETE", "/api/runs/not-a-run", "", "X-Boku", "1"); w.Code != http.StatusNotFound {
		t.Errorf("delete non-run = %d, want 404", w.Code)
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("non-run directory was removed")
	}
}

func TestHTMLIsSandboxed(t *testing.T) {
	_, h, dir := newServer(t)
	_ = os.MkdirAll(filepath.Join(dir, "reports"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "reports", "r.html"), []byte("<script>1</script>"), 0o644)
	w := do(h, "GET", "/api/reports/file?name=r.html", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("code=%d csp=%q", w.Code, w.Header().Get("Content-Security-Policy"))
	}
}

func TestTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	var off int64
	var partial string
	_ = os.WriteFile(p, []byte("one\ntwo\nthr"), 0o644)
	if got := tail(p, &off, &partial); strings.Join(got, "|") != "one|two" {
		t.Errorf("first tail = %v", got)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("ee\n")
	_ = f.Close()
	if got := tail(p, &off, &partial); strings.Join(got, "|") != "three" {
		t.Errorf("second tail = %v", got)
	}
}
