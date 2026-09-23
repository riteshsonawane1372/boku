package render

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// PDFPrinter prints an HTML file to PDF with headless Chrome/Chromium.
//
// Chrome's print engine supports CSS paged media (page size, named pages and
// margin boxes for running headers and page numbers), so the whole layout
// lives in the stylesheet and output is reproducible for a given Chrome build.
type PDFPrinter struct {
	// Browser is the Chrome/Chromium executable; empty autodetects.
	Browser string
	Timeout time.Duration
}

// FindChrome locates a Chrome or Chromium executable. Order: explicit path,
// $BOKU_CHROME, well-known install locations, then $PATH.
func FindChrome(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		if p, err := exec.LookPath(explicit); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("chrome not found at %q", explicit)
	}
	if env := os.Getenv("BOKU_CHROME"); env != "" {
		return FindChrome(env)
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if base != "" {
				candidates = append(candidates,
					filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
					filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`))
			}
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "msedge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chrome or Chromium found: install one, set report.chrome in boku.yaml, or set BOKU_CHROME")
}

// Print renders htmlPath to pdfPath.
//
// Chrome is driven over the DevTools protocol on a private pipe
// (--remote-debugging-pipe) rather than with --print-to-pdf: on some systems the
// headless browser lingers after printing, and the pipe lets Boku wait for
// fonts, know exactly when printing finished, and close the browser itself.
func (p *PDFPrinter) Print(ctx context.Context, htmlPath, pdfPath string) error {
	browser, err := FindChrome(p.Browser)
	if err != nil {
		return err
	}
	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return err
	}
	absPDF, err := filepath.Abs(pdfPath)
	if err != nil {
		return err
	}
	profile, err := os.MkdirTemp("", "boku-chrome-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)

	timeout := p.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// fd 3: commands to Chrome; fd 4: responses from Chrome.
	toChromeR, toChromeW, err := os.Pipe()
	if err != nil {
		return err
	}
	fromChromeR, fromChromeW, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command(browser,
		"--headless=new",
		"--remote-debugging-pipe",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-sync",
		"--hide-scrollbars",
		"--mute-audio",
		"--user-data-dir="+profile,
		"about:blank",
	)
	cmd.ExtraFiles = []*os.File{toChromeR, fromChromeW}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start chrome: %w", err)
	}
	toChromeR.Close()
	fromChromeW.Close()
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() {
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
		toChromeW.Close()
		fromChromeR.Close()
	}()

	c := newCDP(toChromeW, fromChromeR)
	fail := func(step string, err error) error {
		if ctx.Err() != nil {
			return fmt.Errorf("chrome timed out after %s during %s", timeout, step)
		}
		return fmt.Errorf("chrome %s: %v %s", step, err, lastLines(stderr.String(), 3))
	}

	var target struct {
		TargetID string `json:"targetId"`
	}
	if err := c.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &target); err != nil {
		return fail("create target", err)
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &attached); err != nil {
		return fail("attach", err)
	}
	sid := attached.SessionID
	if err := c.call(ctx, sid, "Page.enable", nil, nil); err != nil {
		return fail("enable page", err)
	}
	loaded := c.waitEvent(sid, "Page.loadEventFired")
	if err := c.call(ctx, sid, "Page.navigate", map[string]any{"url": "file://" + filepath.ToSlash(absHTML)}, nil); err != nil {
		return fail("navigate", err)
	}
	select {
	case <-loaded:
	case <-ctx.Done():
		return fail("load", ctx.Err())
	}
	if err := c.call(ctx, sid, "Runtime.evaluate", map[string]any{"expression": "document.fonts.ready.then(() => true)", "awaitPromise": true}, nil); err != nil {
		return fail("wait for fonts", err)
	}
	var pdf struct {
		Data string `json:"data"`
	}
	if err := c.call(ctx, sid, "Page.printToPDF", map[string]any{
		"printBackground":         true,
		"preferCSSPageSize":       true,
		"displayHeaderFooter":     false,
		"generateDocumentOutline": true,
		"generateTaggedPDF":       true,
	}, &pdf); err != nil {
		return fail("print", err)
	}
	_ = c.call(ctx, "", "Browser.close", nil, nil)

	data, err := base64.StdEncoding.DecodeString(pdf.Data)
	if err != nil || len(data) == 0 {
		return fmt.Errorf("chrome returned no PDF data")
	}
	tmp := absPDF + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, absPDF)
}

// cdp is a minimal Chrome DevTools Protocol client over the debugging pipe.
// Messages are JSON objects terminated by a NUL byte.
type cdp struct {
	w      io.Writer
	mu     sync.Mutex
	nextID int
	calls  map[int]chan cdpMessage
	events map[string][]chan struct{}
}

type cdpMessage struct {
	ID        int             `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func newCDP(w io.Writer, r io.Reader) *cdp {
	c := &cdp{w: w, calls: map[int]chan cdpMessage{}, events: map[string][]chan struct{}{}}
	go c.read(r)
	return c
}

func (c *cdp) read(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		raw, err := br.ReadBytes(0)
		if err != nil {
			c.mu.Lock()
			for id, ch := range c.calls {
				close(ch)
				delete(c.calls, id)
			}
			c.mu.Unlock()
			return
		}
		var m cdpMessage
		if json.Unmarshal(raw[:len(raw)-1], &m) != nil {
			continue
		}
		c.mu.Lock()
		if m.ID != 0 {
			if ch, ok := c.calls[m.ID]; ok {
				ch <- m
				delete(c.calls, m.ID)
			}
		} else if m.Method != "" {
			key := m.SessionID + "|" + m.Method
			for _, ch := range c.events[key] {
				close(ch)
			}
			delete(c.events, key)
		}
		c.mu.Unlock()
	}
}

// waitEvent returns a channel closed when the event next fires.
func (c *cdp) waitEvent(sessionID, method string) <-chan struct{} {
	ch := make(chan struct{})
	c.mu.Lock()
	key := sessionID + "|" + method
	c.events[key] = append(c.events[key], ch)
	c.mu.Unlock()
	return ch
}

func (c *cdp) call(ctx context.Context, sessionID, method string, params any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpMessage, 1)
	c.calls[id] = ch
	c.mu.Unlock()

	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := c.w.Write(append(b, 0)); err != nil {
		return err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return errors.New("browser connection closed")
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		if out != nil {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
