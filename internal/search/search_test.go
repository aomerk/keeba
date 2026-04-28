package search

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"basic", "Hello, world! This is keeba.", []string{"hello", "world", "keeba"}},
		{"drops single-char", "I am a Go fan", []string{"go", "fan"}},
		{"drops stopwords", "the quick brown fox", []string{"quick", "brown", "fox"}},
		{"unicode letters", "café résumé", []string{"café", "résumé"}},
		{"digits ok", "go1.22 vs go2", []string{"go1", "22", "vs", "go2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

const validFM = "---\ntags: [test]\nlast_verified: 2026-04-28\nstatus: current\n---\n\n"

func corpus(t *testing.T) config.KeebaConfig {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "auth.md"),
		validFM+"# Authentication\n\n> JWT-based session handling.\n\nThe service issues JWT tokens after login.\nTokens expire after 24h.\n\n## Sources\n\n## See Also\n")
	writeFile(t, filepath.Join(root, "concepts", "billing.md"),
		validFM+"# Billing\n\n> Stripe-based recurring billing.\n\nThe billing service charges via Stripe webhooks.\n\n## Sources\n\n## See Also\n")
	writeFile(t, filepath.Join(root, "concepts", "deployment.md"),
		validFM+"# Deployment\n\n> Kubernetes-based deployment.\n\nWe ship via Helm charts.\nThe rollout is gated by ArgoCD.\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = root
	return cfg
}

func TestBuildIndex(t *testing.T) {
	idx, err := Build(corpus(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if idx.N() != 3 {
		t.Fatalf("N = %d, want 3", idx.N())
	}
}

func TestQueryRanksRelevantFirst(t *testing.T) {
	idx, err := Build(corpus(t))
	if err != nil {
		t.Fatal(err)
	}
	hits := idx.Query("JWT tokens", 5)
	if len(hits) == 0 {
		t.Fatal("no hits for 'JWT tokens'")
	}
	if !strings.Contains(hits[0].Title, "Authentication") {
		t.Fatalf("expected auth page top, got %v", hits)
	}
}

func TestQueryReturnsEmptyForNoMatch(t *testing.T) {
	idx, err := Build(corpus(t))
	if err != nil {
		t.Fatal(err)
	}
	hits := idx.Query("zebra giraffe alpaca", 5)
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %v", hits)
	}
}

func TestQueryRespectsTopK(t *testing.T) {
	idx, err := Build(corpus(t))
	if err != nil {
		t.Fatal(err)
	}
	hits := idx.Query("the service", 1)
	if len(hits) > 1 {
		t.Fatalf("expected ≤1 hit, got %d", len(hits))
	}
}

func TestSnippetIncludesQueryTerm(t *testing.T) {
	idx, err := Build(corpus(t))
	if err != nil {
		t.Fatal(err)
	}
	hits := idx.Query("Helm charts", 1)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if !strings.Contains(strings.ToLower(hits[0].Snippet), "helm") {
		t.Fatalf("snippet missing 'helm': %q", hits[0].Snippet)
	}
}

func TestQueryEmptyIndex(t *testing.T) {
	cfg := config.Defaults()
	cfg.WikiRoot = t.TempDir()
	idx, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if idx.N() != 0 {
		t.Fatalf("N = %d", idx.N())
	}
	if hits := idx.Query("anything", 5); len(hits) != 0 {
		t.Fatalf("expected no hits, got %v", hits)
	}
}
