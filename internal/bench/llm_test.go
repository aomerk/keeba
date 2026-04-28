package bench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/llm"
)

// fakeEval is a deterministic Evaluator used by tests.
type fakeEval struct {
	wikiTokens int
	rawTokens  int
	calls      int
}

func (f *fakeEval) Provider() string { return "fake" }
func (f *fakeEval) Model() string    { return "fake-1" }

func (f *fakeEval) Answer(_ context.Context, q, ctxBlob string) (llm.Answer, error) {
	f.calls++
	tokens := f.wikiTokens
	conf := 4
	if len(ctxBlob) > 5000 {
		tokens = f.rawTokens
		conf = 3
	}
	return llm.Answer{
		Text:         "Answer to " + q + "\nConfidence: " + string(rune('0'+conf)) + "/5",
		InputTokens:  tokens,
		OutputTokens: 30,
		Wall:         5 * time.Millisecond,
		Confidence:   conf,
	}, nil
}

func writeFileLLM(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func corpusForLLM(t *testing.T) config.KeebaConfig {
	root := t.TempDir()
	writeFileLLM(t, filepath.Join(root, "concepts", "auth.md"),
		"---\ntags: [test]\nlast_verified: 2026-04-28\nstatus: current\n---\n\n# Auth\n\n> JWT-based.\n\n## Sources\n\n## See Also\n")
	writeFileLLM(t, filepath.Join(root, "raw", "src.go"), strings.Repeat("a giant blob of source\n", 1000))
	cfg := config.Defaults()
	cfg.WikiRoot = root
	return cfg
}

func TestRunLLMTracksUsage(t *testing.T) {
	cfg := corpusForLLM(t)
	eval := &fakeEval{wikiTokens: 100, rawTokens: 5000}
	rep, err := RunLLM(context.Background(), cfg, eval,
		[]Question{{ID: "q1", Text: "how does auth work"}},
		[]string{"raw"}, 3, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if eval.calls != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", eval.calls)
	}
	if rep.WikiSum.InputTokens != 100 {
		t.Errorf("wiki input tokens: %d", rep.WikiSum.InputTokens)
	}
	if rep.RawSum.InputTokens != 5000 {
		t.Errorf("raw input tokens: %d", rep.RawSum.InputTokens)
	}
	if r := rep.RatioInputTokens(); r != 0.02 {
		t.Errorf("ratio: %v", r)
	}
}

func TestRunLLMSurfacesError(t *testing.T) {
	cfg := corpusForLLM(t)
	eval := &errEval{err: errors.New("boom")}
	_, err := RunLLM(context.Background(), cfg, eval,
		[]Question{{ID: "q1", Text: "x"}}, nil, 3, 1000)
	if err == nil {
		t.Fatal("expected error")
	}
}

type errEval struct{ err error }

func (e *errEval) Provider() string { return "err" }
func (e *errEval) Model() string    { return "err-1" }
func (e *errEval) Answer(_ context.Context, _, _ string) (llm.Answer, error) {
	return llm.Answer{}, e.err
}

func TestMarkdownLLMShape(t *testing.T) {
	rep := LLMReport{
		When:     time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		Provider: "fake", Model: "fake-1",
		Rows: []LLMRow{{
			Question: Question{Text: "what?"},
			Wiki:     LLMAnswer{InputTokens: 100, OutputTokens: 20, Wall: time.Millisecond, Confidence: 4, Text: "answer A"},
			Raw:      LLMAnswer{InputTokens: 1000, OutputTokens: 20, Wall: 10 * time.Millisecond, Confidence: 3, Text: "answer B"},
		}},
		WikiSum: LLMAggregate{InputTokens: 100, Wall: time.Millisecond},
		RawSum:  LLMAggregate{InputTokens: 1000, Wall: 10 * time.Millisecond},
	}
	md := MarkdownLLM(rep)
	for _, want := range []string{"# Bench (LLM)", "10.0× cheaper", "10.0× faster", "Q1. what?", "answer A", "answer B", "## Sources", "## See Also"} {
		if !strings.Contains(md, want) {
			t.Errorf("md missing %q\n%s", want, md)
		}
	}
}
