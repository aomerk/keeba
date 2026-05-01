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

// refsRepo writes a Go corpus that exercises both type and embed refs
// to a target type "Foo", plus a non-test file for the file-prefix test.
func refsRepo(t *testing.T) *Server {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"src/foo.go": `package src

` + strings.Repeat("// padding line to inflate file size beyond MCP response\n", 50) + `

type Foo struct{ N int }

type Container struct {
	F Foo
}

type Embedder struct {
	Foo
}

func use(f Foo) Foo {
	return Foo{N: 1}
}
`,
		"other/use.go": `package other

` + strings.Repeat("// padding line to inflate file size\n", 50) + `

import "src"

type Wrapper struct{ X src.Foo }
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

func parseRefs(t *testing.T, resp map[string]any) (refs []map[string]any, count int) {
	t.Helper()
	text := mcpText(t, resp)
	var parsed struct {
		Callee string           `json:"callee"`
		Count  int              `json:"count"`
		Refs   []map[string]any `json:"refs"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("decode: %v\n%s", err, text)
	}
	return parsed.Refs, parsed.Count
}

func TestFindRefs_ReturnsTypeAndEmbedKinds(t *testing.T) {
	s := refsRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_refs","arguments":{"name":"Foo"}}}`,
	)
	refs, count := parseRefs(t, resps[0])
	if count == 0 {
		t.Fatalf("want refs to Foo, got 0")
	}
	kinds := map[string]bool{}
	for _, r := range refs {
		kinds[r["kind"].(string)] = true
	}
	if !kinds["type"] {
		t.Errorf("missing type kind in %v", kinds)
	}
	if !kinds["embed"] {
		t.Errorf("missing embed kind in %v", kinds)
	}
}

func TestFindRefs_KindFilter(t *testing.T) {
	s := refsRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_refs","arguments":{"name":"Foo","kinds":["embed"]}}}`,
	)
	refs, count := parseRefs(t, resps[0])
	if count == 0 {
		t.Fatalf("want at least one embed ref, got 0")
	}
	for _, r := range refs {
		if r["kind"] != "embed" {
			t.Errorf("non-embed leaked through filter: %v", r)
		}
	}
}

func TestFindRefs_FilePrefixFilter(t *testing.T) {
	s := refsRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"find_refs","arguments":{"name":"Foo","file":"src/"}}}`,
	)
	refs, _ := parseRefs(t, resps[0])
	for _, r := range refs {
		if !strings.HasPrefix(r["caller_file"].(string), "src/") {
			t.Errorf("file filter leaked: %v", r["caller_file"])
		}
	}
}

func TestFindRefs_NameRequired(t *testing.T) {
	s := refsRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"find_refs","arguments":{}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for missing name")
	}
}

func TestFindRefs_NoSymbolGraph(t *testing.T) {
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
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"find_refs","arguments":{"name":"Foo"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, "keeba compile") {
		t.Errorf("expected compile hint, got %q", text)
	}
}

func TestFindRefs_StatsReceiptRises(t *testing.T) {
	s := refsRepo(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_refs","arguments":{"name":"Foo"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	alt, ret := statsAfter(t, resps)
	if alt <= ret {
		t.Errorf("find_refs: alt=%d not > returned=%d", alt, ret)
	}
}
