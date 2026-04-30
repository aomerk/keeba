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

// testsForRepo writes a Go corpus + matching _test.go files, compiles
// the symbol graph, and returns a Server. Mirrors grepRepo but adds
// test-file fixtures so the call graph finds the test → target edges.
func testsForRepo(t *testing.T, files map[string]string) *Server {
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

// testsForHits decodes the matches array from a tests_for response.
func testsForHits(t *testing.T, resp map[string]any) (matches []map[string]any, count int) {
	t.Helper()
	text := mcpText(t, resp)
	var parsed struct {
		Target  string           `json:"target"`
		Count   int              `json:"count"`
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("decode: %v\n%s", err, text)
	}
	return parsed.Matches, parsed.Count
}

func TestTestsFor_CallerInTestFile(t *testing.T) {
	s := testsForRepo(t, map[string]string{
		"src/foo.go": `package src

func Greet(name string) string {
	return "hi " + name
}
`,
		"src/foo_test.go": `package src

import "testing"

func TestSomething(t *testing.T) {
	_ = Greet("world")
}
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"Greet"}}}`,
	)
	matches, count := testsForHits(t, resps[0])
	if count != 1 {
		t.Fatalf("want 1 match, got %d (%v)", count, matches)
	}
	m := matches[0]
	if m["name"] != "TestSomething" {
		t.Errorf("name=%v want TestSomething", m["name"])
	}
	if !strings.HasSuffix(m["file"].(string), "_test.go") {
		t.Errorf("file=%v want _test.go", m["file"])
	}
	reasons := m["reasons"].([]any)
	if len(reasons) != 1 || reasons[0] != "called" {
		t.Errorf("reasons=%v want [called]", reasons)
	}
}

func TestTestsFor_NameMatchHeuristic(t *testing.T) {
	// The test function never calls the target (different package, say),
	// but its name signals the relationship.
	s := testsForRepo(t, map[string]string{
		"src/foo.go": `package src

func Validate(s string) bool { return s != "" }
`,
		"src/foo_test.go": `package src

import "testing"

func TestValidate(t *testing.T) {
	_ = "no direct call"
}
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"Validate"}}}`,
	)
	matches, count := testsForHits(t, resps[0])
	if count != 1 {
		t.Fatalf("want 1 match by name, got %d (%v)", count, matches)
	}
	reasons := matches[0]["reasons"].([]any)
	hasName := false
	for _, r := range reasons {
		if r == "name_match" {
			hasName = true
		}
	}
	if !hasName {
		t.Errorf("reasons=%v missing name_match", reasons)
	}
}

func TestTestsFor_BothReasonsMerged(t *testing.T) {
	// One test both calls AND name-matches → reasons = [called, name_match].
	s := testsForRepo(t, map[string]string{
		"src/foo.go": `package src

func Add(a, b int) int { return a + b }
`,
		"src/foo_test.go": `package src

import "testing"

func TestAdd(t *testing.T) {
	_ = Add(1, 2)
}
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"Add"}}}`,
	)
	matches, count := testsForHits(t, resps[0])
	if count != 1 {
		t.Fatalf("want 1 deduped match, got %d (%v)", count, matches)
	}
	reasons := matches[0]["reasons"].([]any)
	rs := map[string]bool{}
	for _, r := range reasons {
		rs[r.(string)] = true
	}
	if !rs["called"] || !rs["name_match"] {
		t.Errorf("reasons=%v want both called+name_match", reasons)
	}
}

func TestTestsFor_NonTestCallersExcluded(t *testing.T) {
	// Greet is called from a non-test file. tests_for must NOT return that
	// caller — only test-file callers count.
	s := testsForRepo(t, map[string]string{
		"src/foo.go": `package src

func Greet() string { return "hi" }

func Use() { _ = Greet() }
`,
		"src/foo_test.go": `package src

import "testing"

func TestGreet(t *testing.T) { _ = Greet() }
`,
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"Greet"}}}`,
	)
	matches, _ := testsForHits(t, resps[0])
	for _, m := range matches {
		if m["name"] == "Use" {
			t.Errorf("non-test caller leaked: %v", m)
		}
	}
}

func TestTestsFor_NoMatches(t *testing.T) {
	s := testsForRepo(t, map[string]string{
		"src/foo.go": "package src\nfunc Lonely() {}\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"Lonely"}}}`,
	)
	if resps[0]["error"] != nil {
		t.Fatalf("zero matches must not error: %v", resps[0])
	}
	_, count := testsForHits(t, resps[0])
	if count != 0 {
		t.Errorf("want count=0, got %d", count)
	}
}

func TestTestsFor_NameRequired(t *testing.T) {
	s := testsForRepo(t, map[string]string{
		"src/foo.go": "package src\nfunc Foo() {}\n",
	})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"tests_for","arguments":{}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for missing name")
	}
}

func TestTestsFor_NoSymbolGraph(t *testing.T) {
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
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"Foo"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, "keeba compile") {
		t.Errorf("expected `keeba compile` hint, got %q", text)
	}
}

func TestTestsFor_LimitCaps(t *testing.T) {
	// Five test functions all call Target; limit=2 caps the response.
	files := map[string]string{
		"src/foo.go": "package src\nfunc Target() {}\n",
	}
	for i, n := range []string{"a", "b", "c", "d", "e"} {
		files["src/"+n+"_test.go"] = "package src\nimport \"testing\"\nfunc Test" + strings.ToUpper(n) + "(t *testing.T) { Target() }\n"
		_ = i
	}
	s := testsForRepo(t, files)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"tests_for","arguments":{"name":"Target","limit":2}}}`,
	)
	_, count := testsForHits(t, resps[0])
	if count != 2 {
		t.Errorf("limit=2 not respected, got count=%d", count)
	}
}

func TestIsTestSymbol(t *testing.T) {
	// Pure-function table for the heuristic.
	cases := []struct {
		sym  symbol.Symbol
		want bool
	}{
		{symbol.Symbol{File: "x_test.go", Name: "TestFoo", Kind: "function", Language: "go"}, true},
		{symbol.Symbol{File: "x.go", Name: "Foo", Kind: "function", Language: "go"}, false},
		{symbol.Symbol{File: "tests/test_foo.py", Name: "test_validate", Kind: "function", Language: "py"}, true},
		{symbol.Symbol{File: "src/foo.py", Name: "regular", Kind: "function", Language: "py"}, false},
		{symbol.Symbol{File: "components/foo.test.ts", Name: "describe", Kind: "function", Language: "ts"}, true},
		{symbol.Symbol{File: "components/foo.spec.ts", Name: "it", Kind: "function", Language: "ts"}, true},
		{symbol.Symbol{File: "components/foo.ts", Name: "Component", Kind: "function", Language: "ts"}, false},
		{symbol.Symbol{File: "src/lib.rs", Name: "test_thing", Kind: "function", Language: "rs"}, true},
		{symbol.Symbol{File: "src/test/java/FooTest.java", Name: "testFoo", Kind: "method", Language: "java"}, true},
		{symbol.Symbol{File: "src/main/java/Foo.java", Name: "testFoo", Kind: "method", Language: "java"}, false}, // src/main/ → not a test file
		{symbol.Symbol{File: "src/main/java/Foo.java", Name: "main", Kind: "method", Language: "java"}, false},
	}
	for i, c := range cases {
		got := isTestSymbol(c.sym)
		if got != c.want {
			t.Errorf("case %d (%s/%s): got %v want %v", i, c.sym.File, c.sym.Name, got, c.want)
		}
	}
}
