package symbol

import (
	"reflect"
	"testing"
)

func TestSplitIdent(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"AuthMiddleware", []string{"auth", "middleware", "authmiddleware"}},
		{"auth_token_verifier", []string{"auth", "token", "verifier", "auth_token_verifier"}},
		{"JSONParser", []string{"json", "parser", "jsonparser"}},
		{"validateToken", []string{"validate", "token", "validatetoken"}},
		{"X", []string{"x", "x"}},
		{"", nil},
	}
	for _, c := range cases {
		got := splitIdent(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitIdent(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestProseTokens_DropsStopwordsAndShorts(t *testing.T) {
	got := proseTokens("The auth flow is a JWT-based handler.")
	for _, drop := range []string{"the", "is", "a"} {
		for _, g := range got {
			if g == drop {
				t.Errorf("expected %q dropped, got %v", drop, got)
			}
		}
	}
	want := map[string]bool{"auth": true, "flow": true, "jwt": true, "based": true, "handler": true}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected token %q in %v", g, got)
		}
	}
}

// fixtureSyms gives a small set with overlap so BM25 has work to do.
func fixtureSyms() []Symbol {
	return []Symbol{
		{
			Name: "AuthMiddleware", Kind: "function", File: "auth/mw.go",
			StartLine: 10, Signature: "func AuthMiddleware(next Handler) Handler",
			Doc: "Validates JWT tokens before passing to next handler.", Language: "go",
		},
		{
			Name: "ValidateToken", Kind: "function", File: "auth/token.go",
			StartLine: 5, Signature: "func ValidateToken(t string) error",
			Doc: "Parses and verifies a JWT token signature.", Language: "go",
		},
		{
			Name: "BillingHandler", Kind: "function", File: "billing/h.go",
			StartLine: 1, Signature: "func BillingHandler(w ResponseWriter)",
			Doc: "Charges the customer via Stripe.", Language: "go",
		},
		{
			Name: "StartServer", Kind: "function", File: "cmd/main.go",
			StartLine: 20, Signature: "func StartServer(port int)",
			Doc: "Boots the HTTP listener.", Language: "go",
		},
	}
}

func TestQuery_RanksByRelevance(t *testing.T) {
	idx := BuildBM25Index(fixtureSyms())
	hits := idx.Query("auth", 5)
	if len(hits) == 0 {
		t.Fatalf("expected hits, got 0")
	}
	if hits[0].Symbol.Name != "AuthMiddleware" {
		t.Errorf("expected AuthMiddleware top, got %q", hits[0].Symbol.Name)
	}
	for _, h := range hits {
		if h.Symbol.Name == "BillingHandler" {
			t.Errorf("BillingHandler should not match 'auth', got %v", hits)
		}
	}
}

func TestQuery_DocMatch(t *testing.T) {
	idx := BuildBM25Index(fixtureSyms())
	hits := idx.Query("stripe", 5)
	if len(hits) != 1 || hits[0].Symbol.Name != "BillingHandler" {
		t.Errorf("expected BillingHandler from doc match 'stripe', got %v", hits)
	}
}

func TestQuery_PhraseAndIdentBothMatch(t *testing.T) {
	idx := BuildBM25Index(fixtureSyms())
	// "validate token" prose should match ValidateToken via splitIdent.
	hits := idx.Query("validate token", 5)
	if len(hits) == 0 || hits[0].Symbol.Name != "ValidateToken" {
		t.Errorf("expected ValidateToken top, got %v", hits)
	}
}

func TestQuery_LimitK(t *testing.T) {
	idx := BuildBM25Index(fixtureSyms())
	hits := idx.Query("handler", 1)
	if len(hits) != 1 {
		t.Errorf("expected k=1 enforced, got %d", len(hits))
	}
}

func TestQuery_EmptyAndNil(t *testing.T) {
	if h := (*BM25Index)(nil).Query("x", 5); h != nil {
		t.Errorf("nil idx should return nil, got %v", h)
	}
	idx := BuildBM25Index(fixtureSyms())
	if h := idx.Query("", 5); h != nil {
		t.Errorf("empty query should return nil, got %v", h)
	}
	if h := idx.Query("auth", 0); h != nil {
		t.Errorf("k=0 should return nil, got %v", h)
	}
	if h := idx.Query("nomatchterm9999", 5); h != nil {
		t.Errorf("no-match should return nil, got %v", h)
	}
}

func TestQuery_StableTieBreak(t *testing.T) {
	// Two identical doc-only docs; ties broken by file then line.
	syms := []Symbol{
		{Name: "A", File: "z.go", StartLine: 5, Doc: "shared term"},
		{Name: "B", File: "a.go", StartLine: 5, Doc: "shared term"},
		{Name: "C", File: "a.go", StartLine: 1, Doc: "shared term"},
	}
	idx := BuildBM25Index(syms)
	hits := idx.Query("shared", 3)
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(hits))
	}
	want := []string{"C", "B", "A"} // a.go:1, a.go:5, z.go:5
	for i, h := range hits {
		if h.Symbol.Name != want[i] {
			t.Errorf("tiebreak[%d]=%s, want %s", i, h.Symbol.Name, want[i])
		}
	}
}
