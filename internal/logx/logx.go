// Package logx is Boku's small, readable CLI logger.
//
//	INFO  planner completed  workstreams=4
//	WARN  source could not be retrieved  url=https://…
//
// Lines go to the terminal and, when attached, to the run's log file.
package logx

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	mu      sync.Mutex
	out     io.Writer
	file    io.Writer
	verbose bool
	color   bool
}

// New creates a logger writing to w. Colour is used when w is a terminal.
func New(w io.Writer, verbose bool) *Logger {
	l := &Logger{out: w, verbose: verbose}
	if f, ok := w.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" {
			l.color = true
		}
	}
	return l
}

// Discard returns a logger that writes nothing (for tests).
func Discard() *Logger { return New(io.Discard, false) }

// AttachFile tees all subsequent lines (including debug) to w.
func (l *Logger) AttachFile(w io.Writer) {
	l.mu.Lock()
	l.file = w
	l.mu.Unlock()
}

func (l *Logger) Debug(msg string, kv ...any) { l.log("DEBUG", "\033[2m", msg, kv) }
func (l *Logger) Info(msg string, kv ...any)  { l.log("INFO", "\033[36m", msg, kv) }
func (l *Logger) Warn(msg string, kv ...any)  { l.log("WARN", "\033[33m", msg, kv) }
func (l *Logger) Error(msg string, kv ...any) { l.log("ERROR", "\033[31m", msg, kv) }

// Step prints a progress line such as "[2/7] Researching".
func (l *Logger) Step(n, total int, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := fmt.Sprintf("[%d/%d] %s", n, total, msg)
	if l.color {
		fmt.Fprintf(l.out, "\n\033[1m%s\033[0m\n", line)
	} else {
		fmt.Fprintf(l.out, "\n%s\n", line)
	}
	if l.file != nil {
		fmt.Fprintf(l.file, "%s %s\n", time.Now().Format(time.RFC3339), line)
	}
}

// Plain prints an unprefixed line to the terminal.
func (l *Logger) Plain(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, format+"\n", args...)
}

func (l *Logger) log(level, color, msg string, kv []any) {
	var b strings.Builder
	b.WriteString(msg)
	for i := 0; i+1 < len(kv); i += 2 {
		v := fmt.Sprint(kv[i+1])
		if strings.ContainsAny(v, " \t") {
			v = fmt.Sprintf("%q", v)
		}
		fmt.Fprintf(&b, "  %v=%s", kv[i], v)
	}
	line := b.String()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		fmt.Fprintf(l.file, "%s %-5s %s\n", time.Now().Format(time.RFC3339), level, line)
	}
	if level == "DEBUG" && !l.verbose {
		return
	}
	if l.color {
		fmt.Fprintf(l.out, "%s%-5s\033[0m %s\n", color, level, line)
	} else {
		fmt.Fprintf(l.out, "%-5s %s\n", level, line)
	}
}
