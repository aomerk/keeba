package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tinyGoRepo writes a minimal multi-file Go corpus that exercises every
// tool the bench drives — Greet for find_def, an http handler comment
// for search_symbols, an os.Getenv literal for grep_symbols, a function
// call for find_callers, and a TestGreet for tests_for.
func tinyGoRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"main.go": `package main

import "os"

// Greet renders an http handler greeting.
func Greet(name string) string {
	return "hi " + name + " " + os.Getenv("USER")
}

func main() {
	_ = Greet("world")
}

func Run() error { return nil }
`,
		"main_test.go": `package main

import "testing"

func TestGreet(t *testing.T) {
	if got := Greet("x"); got == "" {
		t.Fatal("empty")
	}
}
`,
		"cmd/extra.go": `package cmd

func Helper() {}
`,
	}
	for path, body := range files {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestRunMCPBench_PopulatesReport(t *testing.T) {
	repo := tinyGoRepo(t)
	rep, err := RunMCPBench(repo, nil)
	if err != nil {
		t.Fatalf("RunMCPBench: %v", err)
	}
	if rep.SymbolCount == 0 {
		t.Errorf("SymbolCount=0, want >0")
	}
	if rep.FileCount == 0 {
		t.Errorf("FileCount=0, want >0")
	}
	if rep.IndexBytes == 0 {
		t.Errorf("IndexBytes=0, want >0 — .keeba/symbols.json should exist")
	}
	if rep.CompileMs < 0 {
		t.Errorf("CompileMs negative: %d", rep.CompileMs)
	}
	if len(rep.Queries) != len(DefaultMCPQueries) {
		t.Errorf("Queries len=%d, want %d", len(rep.Queries), len(DefaultMCPQueries))
	}
}

func TestRunMCPBench_QueriesExecute(t *testing.T) {
	repo := tinyGoRepo(t)
	rep, err := RunMCPBench(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Greet is in the corpus — find_def for Greet (not "main") must hit.
	// Default suite uses "main" for find_def; that ALSO exists. Make sure
	// at least one query returned bytes.
	hasReturned := false
	for _, q := range rep.Queries {
		if q.Error != "" {
			t.Errorf("query %q errored: %s", q.Label, q.Error)
		}
		if q.BytesReturned > 0 {
			hasReturned = true
		}
	}
	if !hasReturned {
		t.Errorf("no query returned any bytes — bench is dead in the water")
	}
}

func TestRunMCPBench_AlternativeRatioFromGrep(t *testing.T) {
	repo := tinyGoRepo(t)
	rep, err := RunMCPBench(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	// grep_symbols has an alternative-bytes computer; with a corpus
	// that contains os.Getenv (and context.Context isn't there but the
	// regex still walks every file's body), TotalAlternative must be
	// strictly > 0.
	if rep.TotalAlternative == 0 {
		t.Errorf("TotalAlternative=0, expected grep_symbols to claim some")
	}
	if rep.TotalReturned == 0 {
		t.Errorf("TotalReturned=0, no bytes recorded at all")
	}
	if rep.AlternativeRatio == 0 {
		t.Errorf("AlternativeRatio=0, expected > 0")
	}
}

func TestRunMCPBench_CustomQueriesOverride(t *testing.T) {
	repo := tinyGoRepo(t)
	custom := []MCPQuery{
		{Label: "only one", Tool: "find_def", Args: map[string]any{"name": "Greet"}},
	}
	rep, err := RunMCPBench(repo, custom)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Queries) != 1 {
		t.Errorf("want 1 query, got %d", len(rep.Queries))
	}
	if rep.Queries[0].Label != "only one" {
		t.Errorf("custom label dropped: %v", rep.Queries[0])
	}
}

func TestMarkdownMCPBench_StableHeadlines(t *testing.T) {
	repo := tinyGoRepo(t)
	rep, err := RunMCPBench(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := MarkdownMCPBench(rep)
	for _, want := range []string{
		"# keeba MCP bench",
		"## Index",
		"## Receipt",
		"## Per-query",
		"| Symbols |",
		"bytes_returned",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n----\n%s", want, md)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KiB"},
		{1500, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{int64(1024*1024) * 5 / 2, "2.5 MiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d)=%q want %q", c.n, got, c.want)
		}
	}
}
