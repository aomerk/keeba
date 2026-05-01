package mcp

import (
	"strings"
	"sync"
	"testing"

	"github.com/aomerk/keeba/internal/symbol"
)

func mkSym(name, file string, line int) symbol.Symbol {
	return symbol.Symbol{
		Name:      name,
		File:      file,
		StartLine: line,
		EndLine:   line + 5,
		Kind:      "function",
		Signature: "func " + name + "()",
		Doc:       name + " does a thing.",
		Language:  "go",
	}
}

func TestCodeTable_StableAcrossCalls(t *testing.T) {
	ct := newCodeTable()
	a := mkSym("A", "a.go", 1)
	c1 := ct.codeFor(a)
	c2 := ct.codeFor(a)
	if c1 != c2 {
		t.Errorf("same symbol got different codes: %q vs %q", c1, c2)
	}
}

func TestCodeTable_AllocatesInOrder(t *testing.T) {
	ct := newCodeTable()
	if got := ct.codeFor(mkSym("A", "a.go", 1)); got != "s1" {
		t.Errorf("first code = %q want s1", got)
	}
	if got := ct.codeFor(mkSym("B", "b.go", 1)); got != "s2" {
		t.Errorf("second code = %q want s2", got)
	}
	if got := ct.codeFor(mkSym("C", "c.go", 1)); got != "s3" {
		t.Errorf("third code = %q want s3", got)
	}
}

func TestCodeTable_DistinctSymbolsDistinctCodes(t *testing.T) {
	ct := newCodeTable()
	c1 := ct.codeFor(mkSym("X", "a.go", 1))
	c2 := ct.codeFor(mkSym("X", "b.go", 1)) // same name, different file
	c3 := ct.codeFor(mkSym("X", "a.go", 2)) // same file, different line
	if c1 == c2 || c2 == c3 || c1 == c3 {
		t.Errorf("collisions: c1=%s c2=%s c3=%s", c1, c2, c3)
	}
}

func TestCodeTable_ResolveRoundTrip(t *testing.T) {
	ct := newCodeTable()
	a := mkSym("AuthMiddleware", "auth.go", 5)
	code := ct.codeFor(a)
	got, ok := ct.resolve(code)
	if !ok {
		t.Fatalf("resolve %q failed", code)
	}
	if got.Name != "AuthMiddleware" || got.File != "auth.go" || got.StartLine != 5 {
		t.Errorf("resolve mismatch: %+v", got)
	}
}

func TestCodeTable_ResolveUnknownReturnsFalse(t *testing.T) {
	ct := newCodeTable()
	if _, ok := ct.resolve("s99"); ok {
		t.Errorf("unknown code resolved")
	}
}

func TestCodeTable_ConcurrentSafe(t *testing.T) {
	// 10 goroutines registering 100 symbols each. Each goroutine sees
	// the same symbol → same code; the table grows to exactly 100
	// entries (the symbols are shared).
	ct := newCodeTable()
	syms := make([]symbol.Symbol, 100)
	for i := range syms {
		syms[i] = mkSym("S", "f.go", i+1)
	}
	var wg sync.WaitGroup
	codes := make([][]string, 10)
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			out := make([]string, len(syms))
			for i, s := range syms {
				out[i] = ct.codeFor(s)
			}
			codes[g] = out
		}(g)
	}
	wg.Wait()

	// Every goroutine must have produced the same code sequence.
	first := codes[0]
	for g := 1; g < 10; g++ {
		for i := range first {
			if codes[g][i] != first[i] {
				t.Fatalf("race: goroutine %d sym %d got %q, expected %q", g, i, codes[g][i], first[i])
			}
		}
	}
	// Total entries should equal the number of distinct symbols (100).
	if len(ct.entries) != 100 {
		t.Errorf("entries=%d want 100", len(ct.entries))
	}
	// All codes valid.
	for _, c := range first {
		if _, ok := ct.resolve(c); !ok {
			t.Errorf("resolve %q after concurrent allocation failed", c)
		}
	}
}

func TestExpand_RoundTrip(t *testing.T) {
	s := symbolServer(t)
	s.Codec = "lean"

	// First call: find_def in lean mode allocates codes.
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"Greet"}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, `"codec": "lean"`) {
		t.Fatalf("lean codec marker missing:\n%s", text)
	}
	// Pull the first code out of the lean response.
	code := extractFirstCode(t, text)

	// Second call: expand with that code.
	resps = roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"expand","arguments":{"code":"`+code+`"}}}`,
	)
	expandText := mcpText(t, resps[0])
	if !strings.Contains(expandText, `"name": "Greet"`) {
		t.Errorf("expand didn't return Greet:\n%s", expandText)
	}
	if !strings.Contains(expandText, `"signature":`) {
		t.Errorf("expand response missing signature field:\n%s", expandText)
	}
}

func TestExpand_UnknownCodeError(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"expand","arguments":{"code":"s9999"}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for unknown code, got %v", resps[0])
	}
}

func TestExpand_CodeRequired(t *testing.T) {
	s := symbolServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"expand","arguments":{}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for missing code")
	}
}

func TestFindDefLean_SmallerThanFull(t *testing.T) {
	s := symbolServer(t)
	// Full codec
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"S"}}}`,
	)
	full := mcpText(t, resps[0])

	// Lean codec
	s.Codec = "lean"
	s.codes = newCodeTable() // fresh table for fair comparison
	resps = roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_def","arguments":{"name":"S"}}}`,
	)
	lean := mcpText(t, resps[0])

	if len(lean) >= len(full) {
		t.Errorf("lean (%d bytes) not smaller than full (%d bytes)", len(lean), len(full))
	}
}

// extractFirstCode pulls the first "code": "sN" pair out of a JSON
// blob. Cheap regex-by-hand — keeps the test free of regex/JSON
// parsing dependencies it wouldn't otherwise need.
func extractFirstCode(t *testing.T, text string) string {
	t.Helper()
	const marker = `"code": "`
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("no `code` field in lean response:\n%s", text)
	}
	rest := text[i+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("malformed `code` field in lean response:\n%s", text)
	}
	return rest[:end]
}
