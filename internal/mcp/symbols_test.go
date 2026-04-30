package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/symbol"
)

// symbolServer builds an mcp.Server with a precompiled symbol graph
// already on disk, mirroring the post-`keeba compile` state.
func symbolServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()

	// Wiki bits required by search.Build (BM25 over wiki pages still
	// runs even when symbol-tools are the focus of the test).
	writeFile(t, filepath.Join(root, "concepts", "auth.md"),
		validFM+"# Authentication\n\n> JWT-based session handling.\n\n## Sources\n\n## See Also\n")

	idx := symbol.Index{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		RepoRoot:      root,
		NumFiles:      2,
		NumSymbols:    3,
		Symbols: []symbol.Symbol{
			{
				Name: "Greet", Kind: "function", File: "src/foo.go",
				StartLine: 5, EndLine: 7, Signature: "func Greet(name string) error",
				Doc: "Greet says hi.", Language: "go",
			},
			{
				Name: "Server", Kind: "type", File: "src/server.go",
				StartLine: 10, EndLine: 12, Signature: "type Server struct{...}",
				Language: "go",
			},
			{
				Name: "Start", Kind: "method", File: "src/server.go",
				StartLine: 14, EndLine: 16, Signature: "func (*Server) Start() error",
				Receiver: "Server", Language: "go",
			},
		},
	}
	if err := symbol.Save(root, idx); err != nil {
		t.Fatalf("save symbols: %v", err)
	}

	cfg := config.Defaults()
	cfg.WikiRoot = root
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestFindDef_ExactMatch(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"Greet"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"name": "Greet"`) {
		t.Errorf("expected Greet in find_def result, got %q", text)
	}
	if !strings.Contains(text, `"file": "src/foo.go"`) {
		t.Errorf("expected file path, got %q", text)
	}
}

func TestFindDef_CaseInsensitiveContainsFallback(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"serv"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"name": "Server"`) {
		t.Errorf("expected Server matched by 'serv', got %q", text)
	}
}

func TestFindDef_KindFilter(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"S","kind":"method"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"name": "Start"`) {
		t.Errorf("expected Start (method) in result, got %q", text)
	}
	// The Server *type* should be filtered out — the name "Server" can
	// still appear inside Start's signature ("func (*Server) Start()"),
	// so check the structured `"name": "Server"` field instead of a
	// substring on the whole blob.
	if strings.Contains(text, `"name": "Server"`) {
		t.Errorf("Server (type) should be filtered out, got %q", text)
	}
}

func TestFindDef_LanguageFilter(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"x","language":"py"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"count": 0`) {
		t.Errorf("expected zero py results, got %q", text)
	}
}

func TestSummary_FilePrefix(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"summary","arguments":{"file":"src/server.go"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"name": "Server"`) || !strings.Contains(text, `"name": "Start"`) {
		t.Errorf("expected both Server and Start in summary, got %q", text)
	}
	if strings.Contains(text, `"name": "Greet"`) {
		t.Errorf("Greet (different file) should not appear, got %q", text)
	}
}

func TestSummary_SortsByFileThenLine(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"summary","arguments":{}}}`,
	)
	text := mcpText(t, resps[0])
	// foo.go should appear before server.go alphabetically.
	posFoo := strings.Index(text, `"file": "src/foo.go"`)
	posServer := strings.Index(text, `"file": "src/server.go"`)
	if posFoo < 0 || posServer < 0 || posFoo > posServer {
		t.Errorf("expected foo.go before server.go, got %d vs %d", posFoo, posServer)
	}
}

func TestFindCallers_ReturnsEdgesForCallee(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "foo.go"),
		[]byte(`package src

func A() {
	B()
	B()
}

func C() {
	B()
}

func B() {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := symbol.Compile(repo, repo); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Wiki bits required by search.Build.
	writeFile(t, filepath.Join(repo, "concepts", "stub.md"),
		validFM+"# stub\n\n> note\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = repo
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_callers","arguments":{"name":"B"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"callee": "B"`) {
		t.Errorf("expected callee=B header, got %q", text)
	}
	// 3 call sites (two in A, one in C). Order: file, then line.
	if !strings.Contains(text, `"count": 3`) {
		t.Errorf("expected count=3, got %q", text)
	}
	if !strings.Contains(text, `"caller": "A"`) || !strings.Contains(text, `"caller": "C"`) {
		t.Errorf("expected both A and C callers, got %q", text)
	}
}

func TestFindCallers_FilePrefixFilter(t *testing.T) {
	repo := t.TempDir()
	for path, body := range map[string]string{
		"a/foo.go": "package a\nfunc A() { Helper() }\nfunc Helper() {}\n",
		"b/bar.go": "package b\nfunc B() { Helper() }\nfunc Helper() {}\n",
	} {
		full := filepath.Join(repo, path)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte(body), 0o644)
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

	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_callers","arguments":{"name":"Helper","file":"a/"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"caller_file": "a/foo.go"`) {
		t.Errorf("expected only a/foo.go callers, got %q", text)
	}
	if strings.Contains(text, `"caller_file": "b/bar.go"`) {
		t.Errorf("file filter leaked b/ callers, got %q", text)
	}
}

func TestFindCallers_NoSymbolGraph(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "auth.md"),
		validFM+"# Authentication\n\n> JWT-based session handling.\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = root
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"find_callers","arguments":{"name":"foo"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, "keeba compile") {
		t.Errorf("expected `keeba compile` hint when no graph, got %q", text)
	}
}

func TestFindDef_NoSymbolGraph(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "auth.md"),
		validFM+"# Authentication\n\n> JWT-based session handling.\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = root
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"foo"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, "keeba compile") {
		t.Errorf("expected `keeba compile` hint, got %q", text)
	}
}

func TestSearchSymbols_RanksAndReturnsScores(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"search_symbols","arguments":{"query":"server start"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"query": "server start"`) {
		t.Errorf("expected query echo, got %q", text)
	}
	// Start is a method on Server with both terms — should outrank lone Server.
	posStart := strings.Index(text, `"name": "Start"`)
	posServer := strings.Index(text, `"name": "Server"`)
	if posStart < 0 {
		t.Fatalf("expected Start in hits, got %q", text)
	}
	if posServer >= 0 && posStart > posServer {
		t.Errorf("expected Start to rank above Server, got Start@%d Server@%d", posStart, posServer)
	}
	if !strings.Contains(text, `"score":`) {
		t.Errorf("expected score field, got %q", text)
	}
}

func TestSearchSymbols_KindFilter(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"search_symbols","arguments":{"query":"server","kind":"method"}}}`,
	)
	text := mcpText(t, resps[0])
	if strings.Contains(text, `"name": "Server"`) {
		t.Errorf("Server (type) should be filtered out by kind=method, got %q", text)
	}
	if !strings.Contains(text, `"name": "Start"`) {
		t.Errorf("expected Start (method), got %q", text)
	}
}

func TestSearchSymbols_RequiresQuery(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"search_symbols","arguments":{}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for missing query, got %v", resps[0])
	}
}

func TestSearchSymbols_NoSymbolGraph(t *testing.T) {
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
		`{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"search_symbols","arguments":{"query":"foo"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, "keeba compile") {
		t.Errorf("expected `keeba compile` hint, got %q", text)
	}
}

// mcpText pulls the human-readable text out of an MCP tools/call result.
func mcpText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", resp)
	}
	content := result["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}
