package symbol

import (
	"strings"
	"testing"
)

func TestExtractCalls_GoFreeFunctions(t *testing.T) {
	src := []byte(`package foo

func helper() string { return "x" }

func Greet() {
	helper()
	other.OtherFunc()
}
`)
	edges := ExtractCalls("foo.go", src)
	calleeBy := map[string][]string{} // callee -> list of callers
	for _, e := range edges {
		calleeBy[e.Callee] = append(calleeBy[e.Callee], e.Caller)
	}
	if got := calleeBy["helper"]; len(got) != 1 || got[0] != "Greet" {
		t.Errorf("expected Greet → helper, got %v", calleeBy)
	}
	if got := calleeBy["OtherFunc"]; len(got) != 1 || got[0] != "Greet" {
		t.Errorf("expected Greet → OtherFunc, got %v", calleeBy)
	}
}

func TestExtractCalls_GoMethodReceiver(t *testing.T) {
	src := []byte(`package foo

type Server struct{}

func (s *Server) Start() {
	s.bind()
	s.listen()
}

func (s *Server) bind() {}
func (s *Server) listen() {}
`)
	edges := ExtractCalls("foo.go", src)
	bindCallers := []string{}
	listenCallers := []string{}
	for _, e := range edges {
		switch e.Callee {
		case "bind":
			bindCallers = append(bindCallers, e.Caller)
		case "listen":
			listenCallers = append(listenCallers, e.Caller)
		}
	}
	if len(bindCallers) != 1 || bindCallers[0] != "Start" {
		t.Errorf("bind callers = %v, want [Start]", bindCallers)
	}
	if len(listenCallers) != 1 || listenCallers[0] != "Start" {
		t.Errorf("listen callers = %v, want [Start]", listenCallers)
	}
}

func TestExtractCalls_GoBuiltinsDropped(t *testing.T) {
	src := []byte(`package foo

func Foo() {
	x := make(map[string]int)
	_ = len(x)
	_ = cap(x)
	delete(x, "y")
	helper()
}

func helper() {}
`)
	edges := ExtractCalls("foo.go", src)
	for _, e := range edges {
		switch e.Callee {
		case "make", "len", "cap", "delete", "append":
			t.Errorf("Go builtin %q should not appear as a call edge", e.Callee)
		}
	}
	// helper should be there.
	found := false
	for _, e := range edges {
		if e.Callee == "helper" && e.Caller == "Foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("helper call missing, edges = %v", edges)
	}
}

func TestExtractCalls_PythonRecursionAndMethodCall(t *testing.T) {
	src := []byte(`def fact(n):
    if n <= 1:
        return 1
    return n * fact(n - 1)

def driver():
    helper()
    obj.method()
    fact(5)

def helper():
    pass
`)
	edges := ExtractCalls("foo.py", src)
	calleeCallers := map[string][]string{}
	for _, e := range edges {
		calleeCallers[e.Callee] = append(calleeCallers[e.Callee], e.Caller)
	}
	// Self-recursive fact() call should be skipped (caller==callee).
	if c := calleeCallers["fact"]; len(c) != 1 || c[0] != "driver" {
		t.Errorf("fact callers = %v, want [driver]", c)
	}
	if !contains(calleeCallers["helper"], "driver") {
		t.Errorf("helper callers = %v, want includes driver", calleeCallers["helper"])
	}
	if !contains(calleeCallers["method"], "driver") {
		t.Errorf("method callers = %v, want includes driver", calleeCallers["method"])
	}
}

func TestExtractCalls_TypeScriptArrowFunctionCall(t *testing.T) {
	src := []byte(`export function handler(req) {
  return validate(req);
}

export const validate = (req) => req.ok;
`)
	edges := ExtractCalls("api.ts", src)
	found := false
	for _, e := range edges {
		if e.Callee == "validate" && e.Caller == "handler" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected handler → validate, got %v", edges)
	}
}

func TestExtractCalls_RustFnCallsAcrossImpl(t *testing.T) {
	src := []byte(`pub fn parse(input: &str) -> Vec<i32> {
    tokenize(input)
}

pub fn tokenize(_: &str) -> Vec<i32> { vec![] }
`)
	edges := ExtractCalls("lib.rs", src)
	found := false
	for _, e := range edges {
		if e.Callee == "tokenize" && e.Caller == "parse" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected parse → tokenize, got %v", edges)
	}
}

func TestExtractCalls_DefinitionsNotMistakenForCalls(t *testing.T) {
	src := []byte(`def foo():
    pass

def bar():
    foo()
`)
	edges := ExtractCalls("foo.py", src)
	// Only one edge: bar → foo. The def foo(): line itself shouldn't
	// produce a self-edge.
	if len(edges) != 1 {
		t.Errorf("expected 1 edge (bar → foo), got %d: %v", len(edges), edges)
	}
}

func TestExtractCalls_UnsupportedLanguageNoCrash(t *testing.T) {
	if got := ExtractCalls("README.md", []byte("# Hello\n")); got != nil {
		t.Errorf("unsupported file should return nil, got %v", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestExtractCalls_GoCallerLineIsSiteNotDefSite(t *testing.T) {
	src := []byte(`package foo

func A() {
	B()       // line 4 — this is the call site
}

func B() {}
`)
	edges := ExtractCalls("foo.go", src)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %v", edges)
	}
	if edges[0].CallerLine != 4 {
		t.Errorf("CallerLine = %d, want 4 (the call site)", edges[0].CallerLine)
	}
}

func TestRegexCallNoise_PythonControlFlowDropped(t *testing.T) {
	src := []byte(`def f():
    if x():
        return y()
    while z():
        helper()
`)
	edges := ExtractCalls("f.py", src)
	for _, e := range edges {
		switch e.Callee {
		case "if", "while", "for", "return":
			t.Errorf("control-flow keyword %q leaked as call edge", e.Callee)
		}
	}
	// x, y, z, helper should all be present.
	got := map[string]bool{}
	for _, e := range edges {
		got[e.Callee] = true
	}
	for _, want := range []string{"x", "y", "z", "helper"} {
		if !got[want] {
			t.Errorf("expected callee %q in edges, got %v", want, got)
		}
	}
}

// strip is here so the test file links cleanly even when we expand
// fixtures with whitespace-sensitive helpers.
func strip(s string) string { return strings.TrimSpace(s) }

var _ = strip
