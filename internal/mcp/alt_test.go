package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/symbol"
)

// altRepo writes a small Go corpus with real source files so each
// alt-computer's os.Stat resolves and contributes non-zero bytes.
// The corpus is deliberately bigger than the snippet response will
// be, so bytes_alternative > bytes_returned for every tool we test.
func altRepo(t *testing.T) *Server {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"src/auth.go": `package src

import "os"

// AuthMiddleware validates JWT tokens before passing to next handler.
func AuthMiddleware() string {
	` + strings.Repeat("// padding to inflate file size beyond snippet response\n\t", 30) + `
	return os.Getenv("AUTH_SECRET")
}

func ValidateToken(s string) bool {
	` + strings.Repeat("// padding\n\t", 20) + `
	return s != ""
}
`,
		"src/auth_test.go": `package src

import "testing"

func TestAuthMiddleware(t *testing.T) {
	` + strings.Repeat("// padding\n\t", 30) + `
	_ = AuthMiddleware()
}

func TestValidateToken(t *testing.T) {
	if !ValidateToken("x") {
		t.Fatal("expected true")
	}
}
`,
		"src/billing.go": `package src

func BillingHandler() string {
	` + strings.Repeat("// padding line\n\t", 50) + `
	return "billed"
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
	writeFile(t, filepath.Join(repo, "concepts", "stub.md"),
		validFM+"# stub\n\n> note\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = repo
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// statsAfter pulls bytes_alternative + bytes_returned from a single
// session_stats roundtrip after a query. Helper for the per-tool
// receipt-rises assertions below.
func statsAfter(t *testing.T, resps []map[string]any) (alt, ret int64) {
	t.Helper()
	text := mcpText(t, resps[len(resps)-1])
	var snap map[string]any
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if v, ok := snap["bytes_alternative"].(float64); ok {
		alt = int64(v)
	}
	if v, ok := snap["bytes_returned"].(float64); ok {
		ret = int64(v)
	}
	return alt, ret
}

func TestFindDef_StatsReceiptRises(t *testing.T) {
	s := altRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"AuthMiddleware"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	alt, ret := statsAfter(t, resps)
	if alt <= ret {
		t.Errorf("find_def: alt=%d not > returned=%d", alt, ret)
	}
}

func TestFindCallers_StatsReceiptRises(t *testing.T) {
	s := altRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_callers","arguments":{"name":"ValidateToken"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	alt, ret := statsAfter(t, resps)
	if alt <= ret {
		t.Errorf("find_callers: alt=%d not > returned=%d", alt, ret)
	}
}

func TestSearchSymbols_StatsReceiptRises(t *testing.T) {
	s := altRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_symbols","arguments":{"query":"auth jwt"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	alt, ret := statsAfter(t, resps)
	if alt <= ret {
		t.Errorf("search_symbols: alt=%d not > returned=%d", alt, ret)
	}
}

func TestSummary_StatsReceiptRises(t *testing.T) {
	s := altRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"summary","arguments":{"file":"src/"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	alt, ret := statsAfter(t, resps)
	if alt <= ret {
		t.Errorf("summary: alt=%d not > returned=%d", alt, ret)
	}
}

func TestTestsFor_StatsReceiptRises(t *testing.T) {
	s := altRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"AuthMiddleware"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	alt, ret := statsAfter(t, resps)
	if alt <= ret {
		t.Errorf("tests_for: alt=%d not > returned=%d", alt, ret)
	}
}
