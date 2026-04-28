package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aomerk/keeba/internal/config"
)

const validFM = "---\ntags: [test]\nlast_verified: 2026-04-28\nstatus: current\n---\n\n"

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunProducesNonZero(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "auth.md"),
		validFM+"# Authentication\n\n> JWT tokens.\n\n## Sources\n\n## See Also\n")
	writeFile(t, filepath.Join(root, "raw", "src.go"),
		"// big chunk of source\n"+strings.Repeat("line of code\n", 1000))
	cfg := config.Defaults()
	cfg.WikiRoot = root

	qs := []Question{{ID: "t", Text: "authentication JWT"}}
	rep, err := Run(cfg, qs, []string{"raw"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if rep.N != 1 {
		t.Fatalf("N: %d", rep.N)
	}
	if rep.WikiSum.Tokens == 0 {
		t.Fatalf("wiki tokens 0")
	}
	if rep.RawSum.Tokens == 0 {
		t.Fatalf("raw tokens 0")
	}
	if rep.RatioTokens() <= 0 {
		t.Fatalf("ratio: %v", rep.RatioTokens())
	}
}

func TestMarkdownContainsExpectedShape(t *testing.T) {
	rep := Report{
		When:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		N:       1,
		Wiki:    []Result{{Question: Question{Text: "q"}, Tokens: 100, Wall: time.Millisecond}},
		Raw:     []Result{{Question: Question{Text: "q"}, Tokens: 1000, Wall: 100 * time.Millisecond}},
		WikiSum: AggregateRow{Tokens: 100, Wall: time.Millisecond},
		RawSum:  AggregateRow{Tokens: 1000, Wall: 100 * time.Millisecond},
	}
	md := Markdown(rep)
	for _, want := range []string{"# Bench 2026-04-28", "10.0× cheaper", "100.0× faster", "## Sources", "## See Also"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n%s", want, md)
		}
	}
}

func TestRunEmptyCorpus(t *testing.T) {
	cfg := config.Defaults()
	cfg.WikiRoot = t.TempDir()
	rep, err := Run(cfg, []Question{{ID: "t", Text: "x"}}, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if rep.WikiSum.Tokens != 0 {
		t.Fatalf("wiki tokens should be 0 on empty corpus, got %d", rep.WikiSum.Tokens)
	}
}
