package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

// Web is Boku's own retrieval for providers without built-in web tools
// (ollama, openai). Before a task with web tools runs, Boku searches for the
// task's queries, fetches the result pages and hands their text to the model
// as numbered sources. The model can only cite pages Boku actually fetched.
type Web struct {
	// Engine is "duckduckgo" (default) or "searxng".
	Engine     string
	SearxngURL string
	// ResultsPerQuery, MaxPages and PageChars bound the retrieval.
	ResultsPerQuery int
	MaxPages        int
	PageChars       int
	// Client is used for search requests; nil uses a default client.
	Client *http.Client
	// Fetcher is used for page fetches; nil uses a client that refuses
	// loopback, private and link-local addresses.
	Fetcher *http.Client
}

// Page is a fetched web page.
type Page struct {
	Ref       string `json:"ref"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Publisher string `json:"publisher,omitempty"`
	Published string `json:"published_at,omitempty"`
	Text      string `json:"-"`
}

// SearchResult is one hit from a search engine.
type SearchResult struct {
	URL   string
	Title string
}

const userAgent = "Mozilla/5.0 (compatible; Boku research agent; +https://github.com/riteshsonawane1372/boku)"

// Gather searches every query, fetches up to MaxPages distinct result pages
// and returns those that yielded text, numbered s1, s2, …
func (w *Web) Gather(ctx context.Context, queries []string) ([]Page, error) {
	per := max(1, w.ResultsPerQuery)
	var urls []SearchResult
	seen := map[string]bool{}
	var lastErr error
	// Take results round-robin so every query contributes before any query's
	// lower-ranked results.
	lists := make([][]SearchResult, 0, len(queries))
	for _, q := range queries {
		if strings.TrimSpace(q) == "" {
			continue
		}
		res, err := w.Search(ctx, q)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if len(res) > per {
			res = res[:per]
		}
		lists = append(lists, res)
	}
	for i := 0; i < per; i++ {
		for _, l := range lists {
			if i < len(l) && !seen[l[i].URL] {
				seen[l[i].URL] = true
				urls = append(urls, l[i])
			}
		}
	}
	if len(urls) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("web search: %w", lastErr)
		}
		return nil, errors.New("web search returned no results")
	}
	// Fetch more candidates than needed: some pages fail or have no text.
	limit := max(1, w.MaxPages)
	if len(urls) > limit*2 {
		urls = urls[:limit*2]
	}
	pages := make([]*Page, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, r := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p, err := w.Fetch(ctx, r.URL)
			if err != nil || len(p.Text) < 200 {
				return
			}
			if p.Title == "" {
				p.Title = r.Title
			}
			pages[i] = p
		}()
	}
	wg.Wait()
	var out []Page
	for _, p := range pages {
		if p != nil && len(out) < limit {
			p.Ref = fmt.Sprintf("s%d", len(out)+1)
			out = append(out, *p)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no search result could be fetched")
	}
	return out, nil
}

// Search runs one query on the configured engine.
func (w *Web) Search(ctx context.Context, q string) ([]SearchResult, error) {
	if w.Engine == "searxng" {
		return w.searxng(ctx, q)
	}
	return w.duckduckgo(ctx, q)
}

func (w *Web) client() *http.Client {
	if w.Client != nil {
		return w.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (w *Web) duckduckgo(ctx context.Context, q string) ([]SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://lite.duckduckgo.com/lite/", strings.NewReader(url.Values{"q": {q}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := w.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo returned HTTP %d", resp.StatusCode)
	}
	return ParseDuckDuckGo(io.LimitReader(resp.Body, 2<<20))
}

// ParseDuckDuckGo extracts results from DuckDuckGo's lite HTML page.
func ParseDuckDuckGo(r io.Reader) ([]SearchResult, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	var out []SearchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "result-link") {
			if u := resolveDDG(attr(n, "href")); u != "" {
				out = append(out, SearchResult{URL: u, Title: collapse(textOf(n))})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out, nil
}

// resolveDDG unwraps DuckDuckGo redirect links and drops ads.
func resolveDDG(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Host, "duckduckgo.com") {
		if strings.HasPrefix(u.Path, "/y.js") {
			return "" // advertisement
		}
		if t := u.Query().Get("uddg"); t != "" {
			return resolveDDG(t)
		}
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func (w *Web) searxng(ctx context.Context, q string) ([]SearchResult, error) {
	u := strings.TrimRight(w.SearxngURL, "/") + "/search?" + url.Values{"q": {q}, "format": {"json"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := w.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned HTTP %d (is format=json enabled?)", resp.StatusCode)
	}
	var body struct {
		Results []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, err
	}
	var out []SearchResult
	for _, r := range body.Results {
		out = append(out, SearchResult{URL: r.URL, Title: r.Title})
	}
	return out, nil
}

// Fetch downloads a page and extracts its readable text.
func (w *Web) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("refusing to fetch %q", rawURL)
	}
	client := w.Fetcher
	if client == nil {
		client = publicClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,text/plain;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	body := io.LimitReader(resp.Body, 3<<20)
	p := &Page{URL: resp.Request.URL.String(), Publisher: publisherOf(resp.Request.URL)}
	switch {
	case strings.Contains(ct, "text/plain"):
		b, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		p.Text = collapse(string(b))
	case ct == "" || strings.Contains(ct, "html"):
		if err := extractPage(body, p); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported content type %q", ct)
	}
	if n := max(500, w.PageChars); len(p.Text) > n {
		p.Text = truncateUTF8(p.Text, n) + " …"
	}
	return p, nil
}

// skipTags hold navigation, scripts and chrome rather than content.
var skipTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "svg": true, "nav": true, "footer": true,
	"header": true, "aside": true, "form": true, "iframe": true, "template": true, "button": true,
}

// blockTags end a line of text.
var blockTags = map[string]bool{
	"p": true, "div": true, "li": true, "tr": true, "br": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "section": true, "article": true, "pre": true, "blockquote": true, "table": true,
}

func extractPage(r io.Reader, p *Page) error {
	doc, err := html.Parse(r)
	if err != nil {
		return err
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if p.Title == "" {
					p.Title = collapse(textOf(n))
				}
				return
			case "meta":
				switch attr(n, "property") + attr(n, "name") + attr(n, "itemprop") {
				case "article:published_time", "datePublished", "date", "publish-date", "dc.date":
					if p.Published == "" {
						p.Published = datePrefix(attr(n, "content"))
					}
				case "og:site_name":
					if s := collapse(attr(n, "content")); s != "" {
						p.Publisher = s
					}
				}
			case "time":
				if p.Published == "" {
					p.Published = datePrefix(attr(n, "datetime"))
				}
			}
			if skipTags[n.Data] {
				return
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blockTags[n.Data] {
			b.WriteString("\n")
		}
	}
	walk(doc)
	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		// Drop menu fragments: keep lines that read like sentences or data.
		if l = collapse(l); len(l) >= 40 || (len(l) >= 12 && strings.ContainsAny(l, "0123456789")) {
			lines = append(lines, l)
		}
	}
	p.Text = strings.Join(lines, "\n")
	return nil
}

func datePrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return ""
}

func publisherOf(u *url.URL) string {
	return strings.TrimPrefix(u.Hostname(), "www.")
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// publicClient refuses connections to loopback, private, link-local and other
// non-public addresses: pages are chosen from untrusted search results, so a
// fetch must never reach services on the user's machine or network.
var publicClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(network, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil || !IsPublicIP(ip) {
					return fmt.Errorf("refusing to connect to non-public address %s", host)
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirect to non-HTTP URL")
		}
		return nil
	},
}

// IsPublicIP reports whether ip is a globally routable unicast address.
func IsPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 carrier-grade NAT, 0.0.0.0/8
		if v4[0] == 0 || (v4[0] == 100 && v4[1]&0xC0 == 64) {
			return false
		}
	}
	return true
}
