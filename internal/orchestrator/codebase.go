package orchestrator

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/riteshsonawane1372/boku/internal/agent"
)

// codeTools are the read-only file tools agents get when explaining a codebase.
var codeTools = []string{agent.ToolRead, agent.ToolGlob, agent.ToolGrep}

// tools returns the tools for research and fact-check tasks: the web, plus
// the repository when explaining a codebase.
func (o *Orchestrator) tools() []string {
	if o.Config.Research.Codebase == "" {
		return webTools
	}
	return append(append([]string{}, codeTools...), webTools...)
}

// usesFiles reports whether a task was granted file tools.
func usesFiles(tools []string) bool {
	for _, t := range tools {
		if t == agent.ToolRead || t == agent.ToolGlob || t == agent.ToolGrep {
			return true
		}
	}
	return false
}

// CodebaseTopic is the research topic for explaining the repository at dir,
// optionally narrowed to a focus.
func CodebaseTopic(dir, focus string) string {
	topic := "Explain how the " + filepath.Base(dir) + " codebase works"
	if focus = strings.TrimSpace(focus); focus != "" {
		topic += ", focusing on: " + focus
	}
	return topic
}

// Directories never worth showing an agent.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	"__pycache__": true, "venv": true, ".venv": true, "coverage": true,
}

const (
	overviewMaxEntries = 400
	overviewMaxDepth   = 4
	overviewReadme     = 4000
)

// repoOverview describes a repository for the planner and researchers: its
// commit, a file tree and the start of its README. It saves agents the turns
// they would spend finding their way around.
func repoOverview(ctx context.Context, root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s\n", filepath.Base(root))
	if rev := git(ctx, root, "rev-parse", "--short", "HEAD"); rev != "" {
		fmt.Fprintf(&b, "Commit: %s", rev)
		if br := git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); br != "" && br != "HEAD" {
			fmt.Fprintf(&b, " (branch %s)", br)
		}
		b.WriteString("\n")
	}

	var entries []string
	truncated := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		name := d.Name()
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || skipDirs[name] || depth > overviewMaxDepth {
				return filepath.SkipDir
			}
		} else if strings.HasPrefix(name, ".") && name != ".env.example" {
			return nil
		}
		if len(entries) == overviewMaxEntries {
			truncated = true
			return filepath.SkipAll
		}
		if d.IsDir() {
			rel += "/"
		}
		entries = append(entries, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(entries)
	fmt.Fprintf(&b, "\nFiles (depth ≤ %d, hidden and dependency directories omitted):\n", overviewMaxDepth)
	for _, e := range entries {
		b.WriteString(e + "\n")
	}
	if truncated {
		fmt.Fprintf(&b, "… (listing stopped at %d entries; use Glob for the rest)\n", overviewMaxEntries)
	}

	for _, name := range []string{"README.md", "README", "README.rst", "readme.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		s := string(data)
		if len(s) > overviewReadme {
			s = s[:overviewReadme] + "\n…"
		}
		fmt.Fprintf(&b, "\n%s (start):\n%s\n", name, s)
		break
	}
	return b.String()
}

// repoOverview returns the overview of the codebase being explained. Execute
// builds it before any task runs.
func (o *Orchestrator) repoOverview(ctx context.Context, st *state) string {
	if st.repo == "" {
		st.repo = repoOverview(ctx, o.Config.Research.Codebase)
	}
	return st.repo
}

func git(ctx context.Context, dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
