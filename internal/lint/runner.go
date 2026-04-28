package lint

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/config"
)

// RunResult is the aggregate output of a lint or drift sweep.
type RunResult struct {
	Violations []Violation
	Errors     int
	Warnings   int
}

// Run runs the schema rules over a given list of files.
func Run(targets []string, cfg config.KeebaConfig) (RunResult, error) {
	var all []Violation
	for _, t := range targets {
		v, err := RunAll(t, cfg.WikiRoot, cfg.Lint)
		if err != nil {
			return RunResult{}, err
		}
		all = append(all, v...)
	}
	return summarize(all), nil
}

// DriftTargets runs the citation-drift checks over a given list of files.
func DriftTargets(targets []string, cfg config.KeebaConfig) (RunResult, error) {
	var all []Violation
	for _, t := range targets {
		v, err := CheckPage(t, cfg.WikiRoot, cfg.Drift)
		if err != nil {
			return RunResult{}, err
		}
		all = append(all, v...)
	}
	return summarize(all), nil
}

// StagedPages returns the .md files staged for commit under wikiRoot. It
// shells out to git; an empty slice is returned (no error) if git isn't
// available or the directory isn't a repo.
func StagedPages(wikiRoot string) ([]string, error) {
	cmd := exec.Command("git", "-C", wikiRoot, "diff", "--cached", "--name-only", "--diff-filter=ACMR")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if !strings.HasSuffix(line, ".md") {
			continue
		}
		full := filepath.Join(wikiRoot, line)
		paths = append(paths, full)
	}
	return paths, nil
}

// FormatText renders violations as a grouped, human-readable string.
func FormatText(violations []Violation, wikiRoot string) string {
	if len(violations) == 0 {
		return "lint: clean"
	}
	byFile := map[string][]Violation{}
	keys := []string{}
	for _, v := range violations {
		if _, ok := byFile[v.File]; !ok {
			keys = append(keys, v.File)
		}
		byFile[v.File] = append(byFile[v.File], v)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, f := range keys {
		rel := f
		if r, err := filepath.Rel(wikiRoot, f); err == nil {
			rel = r
		}
		fmt.Fprintf(&sb, "\n%s:\n", rel)
		for _, v := range byFile[f] {
			loc := ""
			if v.Line > 0 {
				loc = fmt.Sprintf(":%d", v.Line)
			}
			fmt.Fprintf(&sb, "  [%s] %s%s — %s\n", v.Severity, v.Rule, loc, v.Message)
		}
	}
	counts := map[Severity]int{}
	for _, v := range violations {
		counts[v.Severity]++
	}
	parts := []string{}
	for _, sev := range []Severity{SevError, SevWarning} {
		n := counts[sev]
		if n == 0 {
			continue
		}
		s := "s"
		if n == 1 {
			s = ""
		}
		parts = append(parts, fmt.Sprintf("%d %s%s", n, sev, s))
	}
	fmt.Fprintf(&sb, "\nlint: %s", strings.Join(parts, ", "))
	return sb.String()
}

// FormatJSON renders violations as a JSON array suitable for tooling.
func FormatJSON(violations []Violation, wikiRoot string) (string, error) {
	type row struct {
		File     string `json:"file"`
		Line     int    `json:"line,omitempty"`
		Rule     string `json:"rule"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Autofix  bool   `json:"autofix,omitempty"`
	}
	rows := make([]row, 0, len(violations))
	for _, v := range violations {
		rel := v.File
		if r, err := filepath.Rel(wikiRoot, v.File); err == nil {
			rel = r
		}
		rows = append(rows, row{
			File: rel, Line: v.Line, Rule: v.Rule,
			Severity: string(v.Severity), Message: v.Message, Autofix: v.Autofix,
		})
	}
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func summarize(v []Violation) RunResult {
	r := RunResult{Violations: v}
	for _, x := range v {
		switch x.Severity {
		case SevError:
			r.Errors++
		case SevWarning:
			r.Warnings++
		}
	}
	return r
}
