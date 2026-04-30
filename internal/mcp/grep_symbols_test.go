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

// grepRepo writes a small Go corpus to a temp dir, compiles its symbol
// graph, sets up the wiki bits search.Build needs, and returns a Server.
// Bodies on disk are real, so grep_symbols can read them; symbol StartLine /
// EndLine come from the actual extractor — no hand-rolled offsets that
// might drift.
func grepRepo(t *testing.T, files map[string]string) *Server {
	t.Helper()
	repo := t.TempDir()
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

// grepHits decodes the hits array from an MCP grep_symbols response.
func grepHits(t *testing.T, resp map[string]any) (hits []map[string]any, count int, truncated bool) {
	t.Helper()
	text := mcpText(t, resp)
	var parsed struct {
		Pattern   string           `json:"pattern"`
		Count     int              `json:"count"`
		Truncated bool             `json:"truncated"`
		Hits      []map[string]any `json:"hits"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("decode hits: %v\n%s", err, text)
	}
	return parsed.Hits, parsed.Count, parsed.Truncated
}

func TestGrepSymbols_SingleHitLineColSnippet(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/foo.go": `package src

// Hello says hi.
func Hello() string {
	return "world"
}
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"world"}}}`,
	)
	hits, count, _ := grepHits(t, resps[0])
	if count != 1 || len(hits) != 1 {
		t.Fatalf("expected 1 hit, got count=%d hits=%v", count, hits)
	}
	h := hits[0]
	if h["name"] != "Hello" {
		t.Errorf("name=%v want Hello", h["name"])
	}
	if int(h["line"].(float64)) != 5 {
		t.Errorf("line=%v want 5 (the return line)", h["line"])
	}
	if !strings.Contains(h["snippet"].(string), `"world"`) {
		t.Errorf("snippet missing literal: %v", h["snippet"])
	}
	if int(h["col"].(float64)) <= 0 {
		t.Errorf("col=%v want >0", h["col"])
	}
}

func TestGrepSymbols_MaxPerSymbolCap(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/repeat.go": `package src

func Repeat() {
	x := 1
	x = 2
	x = 3
	x = 4
	x = 5
	x = 6
	_ = x
}
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"x =","max_per_symbol":3}}}`,
	)
	_, count, _ := grepHits(t, resps[0])
	if count != 3 {
		t.Errorf("expected MaxPerSymbol=3 cap, got count=%d", count)
	}
}

func TestGrepSymbols_MultipleSymbolsSameFile(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/many.go": `package src

func A() { needle() }
func B() { needle() }
func C() { needle() }
func needle() {}
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"needle\\("}}}`,
	)
	hits, count, _ := grepHits(t, resps[0])
	if count < 3 {
		t.Errorf("expected at least 3 hits across A,B,C, got %d hits=%v", count, hits)
	}
	names := map[string]bool{}
	for _, h := range hits {
		names[h["name"].(string)] = true
	}
	for _, want := range []string{"A", "B", "C"} {
		if !names[want] {
			t.Errorf("missing hit in %s, names=%v", want, names)
		}
	}
}

func TestGrepSymbols_LimitTruncates(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/many.go": `package src

func A() { needle() }
func B() { needle() }
func C() { needle() }
func D() { needle() }
func needle() {}
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"needle\\(","limit":2}}}`,
	)
	_, count, truncated := grepHits(t, resps[0])
	if count != 2 {
		t.Errorf("expected 2 (limit), got %d", count)
	}
	if !truncated {
		t.Errorf("expected truncated=true")
	}
}

func TestGrepSymbols_LimitNotTruncatedWhenExhausted(t *testing.T) {
	// Exactly two matching symbols, limit=2 — we walked everything, so
	// truncated must be false. Pinning the post-fix semantics: truncated
	// is "we capped early", not "count == limit".
	s := grepRepo(t, map[string]string{
		"src/two.go": `package src

func A() { _ = "needle" }
func B() { _ = "needle" }
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":50,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"needle","limit":2}}}`,
	)
	_, count, truncated := grepHits(t, resps[0])
	if count != 2 {
		t.Errorf("count=%d want 2", count)
	}
	if truncated {
		t.Errorf("truncated=true but every symbol was walked — false positive")
	}
}

func TestGrepSymbols_KindFilter(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/mix.go": `package src

type T struct{}

func (T) Method() { _ = "needle" }
func Free() { _ = "needle" }
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"needle","kind":"method"}}}`,
	)
	hits, count, _ := grepHits(t, resps[0])
	if count == 0 {
		t.Fatalf("expected at least one method hit, got %v", hits)
	}
	for _, h := range hits {
		if h["kind"] != "method" {
			t.Errorf("non-method leaked through filter: %v", h)
		}
	}
}

func TestGrepSymbols_FilePrefixFilter(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"a/foo.go": "package a\nfunc A() { _ = \"needle\" }\n",
		"b/bar.go": "package b\nfunc B() { _ = \"needle\" }\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"needle","file":"a/"}}}`,
	)
	hits, _, _ := grepHits(t, resps[0])
	for _, h := range hits {
		if !strings.HasPrefix(h["file"].(string), "a/") {
			t.Errorf("file filter leaked: %v", h["file"])
		}
	}
}

func TestGrepSymbols_LiteralRegexMeta(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/env.go": "package src\nimport \"os\"\nfunc Env() string { return os.Getenv(\"DATABASE_URL\") }\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"os.Getenv(\"DATABASE_URL\")","literal":true}}}`,
	)
	_, count, _ := grepHits(t, resps[0])
	if count != 1 {
		t.Errorf("expected 1 literal hit, got %d", count)
	}
}

func TestGrepSymbols_InvalidRegex(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/foo.go": "package src\nfunc Foo() {}\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"["}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for bad regex, got %v", resps[0])
	}
	rpcErr := resps[0]["error"].(map[string]any)
	if msg, _ := rpcErr["message"].(string); !strings.Contains(msg, "invalid regex") {
		t.Errorf("expected 'invalid regex' message, got %q", msg)
	}
}

func TestGrepSymbols_NoMatches(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/foo.go": "package src\nfunc Foo() { _ = 42 }\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"zzzznever"}}}`,
	)
	if resps[0]["error"] != nil {
		t.Fatalf("zero matches must not be an error: %v", resps[0])
	}
	_, count, truncated := grepHits(t, resps[0])
	if count != 0 || truncated {
		t.Errorf("want count=0 truncated=false, got count=%d truncated=%v", count, truncated)
	}
}

func TestGrepSymbols_DeletedFileSkipped(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(repo, "src", "gone.go")
	if err := os.WriteFile(gone, []byte("package src\nfunc Gone() { _ = \"needle\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "kept.go"),
		[]byte("package src\nfunc Kept() { _ = \"needle\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := symbol.Compile(repo, repo); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(repo, "concepts", "stub.md"),
		validFM+"# stub\n\n> note\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = repo
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"needle"}}}`,
	)
	if resps[0]["error"] != nil {
		t.Fatalf("missing-file should be silent, got %v", resps[0])
	}
	hits, count, _ := grepHits(t, resps[0])
	if count == 0 {
		t.Fatalf("expected hits in kept.go, got 0")
	}
	for _, h := range hits {
		if strings.Contains(h["file"].(string), "gone.go") {
			t.Errorf("deleted file should not appear: %v", h)
		}
	}
}

func TestGrepSymbols_FileEscapeRejected(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/foo.go": "package src\nfunc Foo() {}\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"x","file":"/etc/passwd"}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for absolute path in file filter")
	}
}

func TestGrepSymbols_PatternRequired(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/foo.go": "package src\nfunc Foo() {}\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"grep_symbols","arguments":{}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for empty pattern")
	}
}

func TestGrepSymbols_PatternTooLong(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/foo.go": "package src\nfunc Foo() {}\n",
	})
	long := strings.Repeat("a", 1025)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 13, "method": "tools/call",
		"params": map[string]any{"name": "grep_symbols", "arguments": map[string]any{"pattern": long}},
	})
	resps := roundTrip(t, s, string(body))
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for oversize pattern")
	}
}

func TestGrepSymbols_NoSymbolGraph(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "x.md"),
		validFM+"# x\n\n> note\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = root
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"foo"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, "keeba compile") {
		t.Errorf("expected `keeba compile` hint, got %q", text)
	}
}

func TestGrepSymbols_StatsReceiptRises(t *testing.T) {
	s := grepRepo(t, map[string]string{
		"src/big.go": "package src\n" + strings.Repeat("// padding line to inflate file size\n", 200) +
			"func WithNeedle() { _ = \"needle\" }\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"grep_symbols","arguments":{"pattern":"needle"}}}`,
		`{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	if len(resps) != 2 {
		t.Fatalf("want 2 responses, got %d", len(resps))
	}
	stats := mcpText(t, resps[1])
	var snap map[string]any
	if err := json.Unmarshal([]byte(stats), &snap); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	calls := snap["calls_by_tool"].(map[string]any)
	if calls["grep_symbols"] == nil {
		t.Errorf("grep_symbols missing from calls_by_tool: %v", calls)
	}
	alt := snap["bytes_alternative"].(float64)
	ret := snap["bytes_returned"].(float64)
	if alt <= ret {
		t.Errorf("expected bytes_alternative > bytes_returned (savings story), got alt=%v ret=%v", alt, ret)
	}
}
