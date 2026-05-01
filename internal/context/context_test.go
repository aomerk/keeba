package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/symbol"
)

func TestExtractIdentifiers_CamelAndSnake(t *testing.T) {
	prompt := "Investigate why MonSetPromote admits sub-threshold addresses; check focus_token_loader logic."
	got := ExtractIdentifiers(prompt)
	want := []string{"MonSetPromote", "focus_token_loader"}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing identifier %q in %v", w, got)
		}
	}
}

func TestExtractIdentifiers_DropsCommonWords(t *testing.T) {
	prompt := "I am investigating cacheKey promotion logic with the auth team"
	got := ExtractIdentifiers(prompt)
	for _, drop := range []string{"I", "the", "with"} {
		for _, g := range got {
			if g == drop {
				t.Errorf("common word %q leaked into idents %v", drop, got)
			}
		}
	}
}

func TestExtractQuoted_DoubleAndSingle(t *testing.T) {
	prompt := `Find sites that read "DATABASE_URL" and the const 'MaxRetries'`
	got := ExtractQuoted(prompt)
	want := []string{"DATABASE_URL", "MaxRetries"}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing quoted literal %q in %v", w, got)
		}
	}
}

func TestExtractQuoted_SkipsTrivial(t *testing.T) {
	// Single chars and very short strings are dropped — too noisy.
	prompt := `Check "a" and "ok" but not "MonSet"`
	got := ExtractQuoted(prompt)
	for _, g := range got {
		if g == "a" || g == "ok" {
			t.Errorf("trivial quoted leaked: %v", got)
		}
	}
	found := false
	for _, g := range got {
		if g == "MonSet" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing MonSet in %v", got)
	}
}

// fixtureRepo writes a small Go corpus + compiles the symbol graph. The
// context Build function loads from .keeba/symbols.json so we drive
// through the same path the CLI uses.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"src/auth.go": `package src

import "os"

// AuthMiddleware validates JWT tokens before passing to next handler.
func AuthMiddleware() string {
	return os.Getenv("AUTH_SECRET")
}
`,
		"src/billing.go": `package src

// BillingHandler charges the customer via Stripe.
func BillingHandler() string {
	return "billed"
}
`,
		"src/auth_test.go": `package src

import "testing"

func TestAuthMiddleware(t *testing.T) {
	_ = AuthMiddleware()
}
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
	if _, err := symbol.Compile(repo, repo); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return repo
}

func TestBuild_FindsByName(t *testing.T) {
	repo := fixtureRepo(t)
	rep, err := Build(repo, "Investigate AuthMiddleware token validation flow", Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The exact-name lookup should find AuthMiddleware.
	hasName := false
	for _, h := range rep.NameHits {
		if h.Symbol.Name == "AuthMiddleware" {
			hasName = true
		}
	}
	if !hasName {
		t.Errorf("AuthMiddleware missing from NameHits: %+v", rep.NameHits)
	}
}

func TestBuild_BM25Hits(t *testing.T) {
	repo := fixtureRepo(t)
	rep, err := Build(repo, "stripe billing customer charge logic", Options{})
	if err != nil {
		t.Fatal(err)
	}
	// BM25 over the doc string should rank BillingHandler.
	hasBilling := false
	for _, h := range rep.BM25Hits {
		if h.Symbol.Name == "BillingHandler" {
			hasBilling = true
		}
	}
	if !hasBilling {
		t.Errorf("BillingHandler missing from BM25Hits: %+v", rep.BM25Hits)
	}
}

func TestBuild_QuotedLiteralGrep(t *testing.T) {
	repo := fixtureRepo(t)
	rep, err := Build(repo, `where do we read "AUTH_SECRET" from env?`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.LiteralHits) == 0 {
		t.Errorf("expected literal hits for AUTH_SECRET, got none")
	}
}

func TestBuild_NoSymbolGraph(t *testing.T) {
	repo := t.TempDir()
	_, err := Build(repo, "anything", Options{})
	if err == nil {
		t.Errorf("expected error when .keeba/symbols.json missing")
	}
}

func TestRenderMarkdown_StableHeadlines(t *testing.T) {
	repo := fixtureRepo(t)
	rep, err := Build(repo, "Investigate AuthMiddleware and stripe billing", Options{})
	if err != nil {
		t.Fatal(err)
	}
	md := RenderMarkdown(rep)
	for _, want := range []string{
		"# keeba context",
		"## Most relevant",
		"## By name",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n----\n%s", want, md)
		}
	}
}

func TestRenderMarkdown_RespectsMaxBytes(t *testing.T) {
	repo := fixtureRepo(t)
	rep, err := Build(repo, "AuthMiddleware billing Stripe handler customer", Options{})
	if err != nil {
		t.Fatal(err)
	}
	full := RenderMarkdown(rep)
	limit := len(full) / 3
	rep2, _ := Build(repo, "AuthMiddleware billing Stripe handler customer", Options{MaxBytes: limit})
	short := RenderMarkdown(rep2)
	if len(short) > limit+200 { // allow tail-marker slop
		t.Errorf("expected short markdown <= %d (with slop), got %d", limit, len(short))
	}
}
