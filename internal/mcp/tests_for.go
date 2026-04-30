package mcp

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/symbol"
)

// testsForArgs is the argument shape for the tests_for tool.
type testsForArgs struct {
	Name  string `json:"name"`
	Limit int    `json:"limit,omitempty"`
}

// testsForMatch is one test that exercises the target symbol. Reasons
// is a small set drawn from {"called", "name_match"} — agents can rank
// by len(reasons) descending to surface the strongest signals first.
type testsForMatch struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	File      string   `json:"file"`
	StartLine int      `json:"start_line"`
	Signature string   `json:"signature,omitempty"`
	Reasons   []string `json:"reasons"`
}

// toolTestsFor returns the test functions exercising a target symbol.
// Two heuristics, merged by (file, line):
//
//	"called"     — the test function is a caller of the target via the
//	               compiled call graph.
//	"name_match" — the test function lives in a test file and its name
//	               contains the target's bare name (TestFoo for Foo,
//	               test_foo for foo, testFoo for foo).
//
// Replaces the find_callers + filter + read_file dance an agent runs
// every time it changes a symbol and wonders "what tests should I run?".
func (s *Server) toolTestsFor(raw json.RawMessage) rpcResponse {
	if s.live == nil {
		return notCompiledResponse()
	}
	var a testsForArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if strings.TrimSpace(a.Name) == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "name is required"}}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 200 {
		limit = 200
	}

	// Index 1: callers in test files. Resolve each edge (CallerFile,
	// Caller) back to the symbol so we can hand back StartLine + signature.
	type key struct {
		File string
		Line int
	}
	merged := map[key]*testsForMatch{}

	for _, e := range s.live.CallersOf(a.Name) {
		if !isTestFile(e.CallerFile) {
			continue
		}
		// Resolve caller name → symbol. ByName may return multiple symbols
		// (overloaded across files); pick the one whose file matches the
		// edge's CallerFile.
		var caller symbol.Symbol
		var found bool
		for _, sym := range s.live.ByName(e.Caller) {
			if sym.File == e.CallerFile {
				caller = sym
				found = true
				break
			}
		}
		if !found {
			continue
		}
		k := key{File: caller.File, Line: caller.StartLine}
		merged[k] = &testsForMatch{
			Name:      caller.Name,
			Kind:      caller.Kind,
			File:      caller.File,
			StartLine: caller.StartLine,
			Signature: caller.Signature,
			Reasons:   []string{"called"},
		}
	}

	// Index 2: name-match heuristic. Walk all symbols, keep test-file
	// symbols whose name signals they exercise the target.
	for _, sym := range s.live.Symbols() {
		if !isTestSymbol(sym) {
			continue
		}
		if !nameMatchesTarget(sym.Name, a.Name) {
			continue
		}
		k := key{File: sym.File, Line: sym.StartLine}
		if existing, ok := merged[k]; ok {
			existing.Reasons = appendUnique(existing.Reasons, "name_match")
			continue
		}
		merged[k] = &testsForMatch{
			Name:      sym.Name,
			Kind:      sym.Kind,
			File:      sym.File,
			StartLine: sym.StartLine,
			Signature: sym.Signature,
			Reasons:   []string{"name_match"},
		}
	}

	// Stable order: stronger signal first (more reasons), then file/line.
	out := make([]testsForMatch, 0, len(merged))
	for _, m := range merged {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Reasons) != len(out[j].Reasons) {
			return len(out[i].Reasons) > len(out[j].Reasons)
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].StartLine < out[j].StartLine
	})
	if len(out) > limit {
		out = out[:limit]
	}

	body, err := json.MarshalIndent(map[string]any{
		"target":  a.Name,
		"count":   len(out),
		"matches": out,
	}, "", "  ")
	if err != nil {
		return rpcResponse{Error: &rpcError{Code: -32603, Message: "encode: " + err.Error()}}
	}
	return rpcResponse{Result: map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(body),
		}},
	}}
}

// isTestFile returns true if path looks like a test file in any of the
// languages keeba extracts symbols from.
func isTestFile(path string) bool {
	p := filepath.ToSlash(path)
	base := filepath.Base(p)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
		return true
	case strings.HasSuffix(base, "_test.py"):
		return true
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.tsx"):
		return true
	case strings.HasSuffix(base, ".spec.ts"), strings.HasSuffix(base, ".spec.tsx"):
		return true
	case strings.HasSuffix(base, ".test.js"), strings.HasSuffix(base, ".spec.js"):
		return true
	case strings.Contains(p, "/__tests__/"), strings.HasPrefix(p, "__tests__/"):
		return true
	case strings.Contains(p, "/src/test/"), strings.HasPrefix(p, "src/test/"): // Maven/Gradle Java convention
		return true
	}
	return false
}

// isTestSymbol applies the test-detection heuristic. Rust is special:
// tests live inline inside `#[cfg(test)] mod tests { ... }` blocks, so
// the only filename-agnostic signal is a `test_` / `Test` name prefix.
// Every other supported language gates on a test-shaped file path first
// and then applies the language's name convention.
func isTestSymbol(sym symbol.Symbol) bool {
	if sym.Kind != "function" && sym.Kind != "method" {
		return false
	}
	if sym.Language == "rs" {
		return strings.HasPrefix(sym.Name, "test_") || strings.HasPrefix(sym.Name, "Test")
	}
	if !isTestFile(sym.File) {
		return false
	}
	switch sym.Language {
	case "go":
		return strings.HasPrefix(sym.Name, "Test") || strings.HasPrefix(sym.Name, "Benchmark") ||
			strings.HasPrefix(sym.Name, "Example") || strings.HasPrefix(sym.Name, "Fuzz")
	case "py":
		return strings.HasPrefix(sym.Name, "test_") || strings.HasPrefix(sym.Name, "Test")
	case "ts", "tsx", "js", "jsx":
		// Anything inside a *.test.ts / *.spec.ts / __tests__/ counts —
		// describe/it/test wrappers don't follow a name prefix.
		return true
	case "java", "kt":
		return strings.HasPrefix(sym.Name, "test") || strings.HasPrefix(sym.Name, "Test")
	}
	return strings.HasPrefix(strings.ToLower(sym.Name), "test")
}

// nameMatchesTarget says the test name suggests it exercises target.
// "TestFoo" matches "Foo"; "test_validate_token" matches "validate_token";
// "testFoo" matches "Foo". Case-insensitive substring after stripping
// the conventional Test/test prefix.
func nameMatchesTarget(testName, target string) bool {
	if target == "" {
		return false
	}
	stripped := testName
	for _, prefix := range []string{"Test", "test_", "test", "Benchmark", "benchmark", "Example", "Fuzz"} {
		if rest, ok := strings.CutPrefix(stripped, prefix); ok {
			stripped = rest
			break
		}
	}
	return strings.Contains(strings.ToLower(stripped), strings.ToLower(target))
}

// appendUnique adds s to slice if not already present.
func appendUnique(slice []string, s string) []string {
	if slices.Contains(slice, s) {
		return slice
	}
	return append(slice, s)
}
