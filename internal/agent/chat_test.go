package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDuckDuckGo(t *testing.T) {
	page := `<html><body><table>
<tr><td><a rel="nofollow" href="https://kubernetes.io/docs/gpus/" class='result-link'>Schedule GPUs</a></td></tr>
<tr><td><a rel="nofollow" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org%2Fa&rut=x" class='result-link'>Example</a></td></tr>
<tr><td><a rel="nofollow" href="https://duckduckgo.com/y.js?ad=1" class='result-link'>Ad</a></td></tr>
<tr><td><a href="https://other.example/" class='nav'>Nav</a></td></tr>
</table></body></html>`
	res, err := ParseDuckDuckGo(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].URL != "https://kubernetes.io/docs/gpus/" || res[0].Title != "Schedule GPUs" || res[1].URL != "https://example.org/a" {
		t.Errorf("unexpected results: %+v", res)
	}
}

func TestIsPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fd00::1": false, "2606:4700::1111": true,
	} {
		if got := IsPublicIP(net.ParseIP(ip)); got != want {
			t.Errorf("IsPublicIP(%s) = %v", ip, got)
		}
	}
}

func TestExtractPage(t *testing.T) {
	p := &Page{}
	err := extractPage(strings.NewReader(`<html><head><title>GPU report</title>
<meta property="article:published_time" content="2026-05-04T10:00:00Z"><meta property="og:site_name" content="CNCF"></head>
<body><nav>Home About Contact</nav><script>var x = 1;</script>
<p>Kubernetes clusters running GPU workloads grew to 48% of respondents in the 2026 survey.</p>
<footer>Copyright notice and other boilerplate text that is long enough</footer></body></html>`), p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "GPU report" || p.Published != "2026-05-04" || p.Publisher != "CNCF" {
		t.Errorf("metadata: %+v", p)
	}
	if !strings.Contains(p.Text, "48% of respondents") || strings.Contains(p.Text, "var x") || strings.Contains(p.Text, "Copyright") {
		t.Errorf("text: %q", p.Text)
	}
}

func TestGroundSources(t *testing.T) {
	pages := []Page{{Ref: "s1", URL: "https://a.example/1", Title: "A", Published: "2026-01-02"}, {Ref: "s2", URL: "https://b.example/2", Title: "B"}, {Ref: "s3", URL: "https://c.example/3", Title: "C"}}
	out := groundSources(json.RawMessage(`{"findings":[{"source_refs":["s1","s9"]},{"source_refs":["s3"]}],"sources":[
		{"ref":"s1","url":"https://hallucinated.example","title":""},
		{"ref":"x9","url":"https://b.example/2","title":"B page"},
		{"ref":"s7","url":"https://made-up.example"}]}`), pages)
	var got struct {
		Sources []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 3 {
		t.Fatalf("want 3 grounded sources, got %v", got.Sources)
	}
	if got.Sources[2]["ref"] != "s3" || got.Sources[2]["url"] != "https://c.example/3" {
		t.Errorf("cited but unlisted fetched page not added: %v", got.Sources[2])
	}
	if got.Sources[0]["url"] != "https://a.example/1" || got.Sources[0]["title"] != "A" || got.Sources[0]["published_at"] != "2026-01-02" {
		t.Errorf("s1 not grounded: %v", got.Sources[0])
	}
	if got.Sources[1]["ref"] != "x9" || got.Sources[1]["url"] != "https://b.example/2" {
		t.Errorf("url match lost: %v", got.Sources[1])
	}
}

func TestChatModelOllama(t *testing.T) {
	var req map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		_, _ = w.Write([]byte(`{"message":{"content":"{\"answer\": 42}"},"prompt_eval_count":1000,"eval_count":500}`))
	}))
	defer srv.Close()
	m := &ChatModel{API: APIOllama, Endpoint: srv.URL, Model: "llama3.1:8b", ContextTokens: 8192, PriceInputPerMTok: 1, PriceOutputPerMTok: 2}
	res, err := m.Run(context.Background(), Task{ID: "t", Role: RoleEditorial, Objective: "o", Instructions: "sys", Schema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"answer": 42}` || res.Usage.CostUSD != 0.002 {
		t.Errorf("result: %s cost %v", res.Output, res.Usage.CostUSD)
	}
	if req["model"] != "llama3.1:8b" || req["format"] == nil || req["options"].(map[string]any)["num_ctx"].(float64) != 8192 {
		t.Errorf("request: %v", req)
	}
}

func TestChatModelOpenAIFallsBackWithoutStructuredOutput(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["response_format"]; ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"response_format not supported"}`))
			return
		}
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"ok\\\": true}\\n```\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}"))
	}))
	defer srv.Close()
	m := &ChatModel{API: APIOpenAI, Endpoint: srv.URL + "/v1", Model: "m", APIKey: "k"}
	res, err := m.Run(context.Background(), Task{ID: "t", Schema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"ok": true}` || calls != 2 {
		t.Errorf("output %s after %d calls", res.Output, calls)
	}
}

func TestChatModelUnreachableIsNotRetryable(t *testing.T) {
	m := &ChatModel{API: APIOllama, Endpoint: "http://127.0.0.1:1", Model: "m"}
	_, err := m.Run(context.Background(), Task{ID: "t"})
	if err == nil || IsRetryable(err) {
		t.Errorf("want non-retryable unavailable error, got %v", err)
	}
}

func TestRouter(t *testing.T) {
	hit := ""
	mk := func(name string) Agent {
		return Func(func(ctx context.Context, task Task) (Result, error) { hit = name; return Result{}, nil })
	}
	r := &Router{Default: mk("claude"), Roles: map[string]Agent{RoleFormatter: mk("local")}}
	_, _ = r.Run(context.Background(), Task{Role: RoleFormatter})
	if hit != "local" {
		t.Error("formatter not routed to local")
	}
	_, _ = r.Run(context.Background(), Task{Role: RoleEditorial})
	if hit != "claude" {
		t.Error("editorial not routed to default")
	}
}

func TestFitPages(t *testing.T) {
	long := strings.Repeat("x", 4000)
	pages := []Page{{Ref: "s1", Text: long}, {Ref: "s2", Text: long}, {Ref: "s3", Text: long}}
	if got := fitPages(pages, 0, 1000); len(got) != 3 || len(got[0].Text) != 4000 {
		t.Error("no context limit should keep pages as they are")
	}
	got := fitPages(pages, 8192, 6000)
	total := 6000
	for _, p := range got {
		total += len(p.Text) + 300
	}
	if total > (8192-4096)*3 || len(got) == 0 || got[0].Ref != "s1" {
		t.Errorf("pages not fitted: %d pages, %d chars", len(got), total)
	}
	if len(pages[0].Text) != 4000 {
		t.Error("fitPages must not modify its input")
	}
}
