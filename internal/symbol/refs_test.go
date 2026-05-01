package symbol

import (
	"sort"
	"testing"
)

// refKey is the dedup-friendly view of one RefEdge.
type refKey struct {
	Caller string
	Callee string
	Kind   string
	Line   int
}

func keysOf(edges []RefEdge) []refKey {
	out := make([]refKey, 0, len(edges))
	for _, e := range edges {
		out = append(out, refKey{e.Caller, e.Callee, e.Kind, e.CallerLine})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func mustHave(t *testing.T, got []RefEdge, want refKey) {
	t.Helper()
	for _, k := range keysOf(got) {
		if k.Caller == want.Caller && k.Callee == want.Callee && k.Kind == want.Kind {
			return
		}
	}
	t.Errorf("missing ref %+v in %v", want, keysOf(got))
}

func mustNotHave(t *testing.T, got []RefEdge, callee, kind string) {
	t.Helper()
	for _, k := range keysOf(got) {
		if k.Callee == callee && k.Kind == kind {
			t.Errorf("unexpected ref to %s/%s in %v", callee, kind, keysOf(got))
			return
		}
	}
}

func TestExtractRefs_TypeInVarParamReturn(t *testing.T) {
	src := []byte(`package src

type Foo struct{}

var x Foo

func uses(f Foo) Foo {
	return f
}
`)
	got := ExtractRefs("src/x.go", src)
	mustHave(t, got, refKey{Caller: "<file>", Callee: "Foo", Kind: "type"}) // var x Foo
	mustHave(t, got, refKey{Caller: "uses", Callee: "Foo", Kind: "type"})   // param
	mustHave(t, got, refKey{Caller: "uses", Callee: "Foo", Kind: "type"})   // return
}

func TestExtractRefs_StructFieldType(t *testing.T) {
	src := []byte(`package src

type Foo struct{}

type Container struct {
	F Foo
}
`)
	got := ExtractRefs("src/x.go", src)
	mustHave(t, got, refKey{Caller: "Container", Callee: "Foo", Kind: "type"})
}

func TestExtractRefs_PointerSliceMap(t *testing.T) {
	src := []byte(`package src

type Foo struct{}

var p *Foo
var s []Foo
var m map[string]Foo
`)
	got := ExtractRefs("src/x.go", src)
	count := 0
	for _, k := range keysOf(got) {
		if k.Callee == "Foo" && k.Kind == "type" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("want 3 type refs to Foo (pointer/slice/map), got %d in %v", count, keysOf(got))
	}
}

func TestExtractRefs_CompositeLiteral(t *testing.T) {
	src := []byte(`package src

type Foo struct{ N int }

func make() Foo {
	return Foo{N: 1}
}
`)
	got := ExtractRefs("src/x.go", src)
	mustHave(t, got, refKey{Caller: "make", Callee: "Foo", Kind: "type"})
}

func TestExtractRefs_TypeAssertion(t *testing.T) {
	src := []byte(`package src

type Foo struct{}

func check(v interface{}) Foo {
	return v.(Foo)
}
`)
	got := ExtractRefs("src/x.go", src)
	count := 0
	for _, k := range keysOf(got) {
		if k.Callee == "Foo" && k.Kind == "type" {
			count++
		}
	}
	// One in return type, one in type assertion.
	if count != 2 {
		t.Errorf("want 2 type refs to Foo, got %d in %v", count, keysOf(got))
	}
}

func TestExtractRefs_EmbedInStructAndInterface(t *testing.T) {
	src := []byte(`package src

type Base struct{}
type Iface interface{}

type Embedder struct {
	Base
}

type Wider interface {
	Iface
}
`)
	got := ExtractRefs("src/x.go", src)
	mustHave(t, got, refKey{Caller: "Embedder", Callee: "Base", Kind: "embed"})
	mustHave(t, got, refKey{Caller: "Wider", Callee: "Iface", Kind: "embed"})
}

func TestExtractRefs_SkipCallExprFun(t *testing.T) {
	src := []byte(`package src

func target() {}

func caller() {
	target()
}
`)
	got := ExtractRefs("src/x.go", src)
	// target() is a call — find_callers handles. find_refs should NOT
	// emit a ref for the function name in CallExpr.Fun position.
	mustNotHave(t, got, "target", "value")
	mustNotHave(t, got, "target", "type")
}

func TestExtractRefs_SkipDeclaringIdent(t *testing.T) {
	src := []byte(`package src

type Foo struct{}

func Bar() {}
`)
	got := ExtractRefs("src/x.go", src)
	// Foo is declared on the type spec; Bar is declared on the FuncDecl.
	// Neither name should appear as a ref to itself.
	for _, k := range keysOf(got) {
		if (k.Callee == "Foo" || k.Callee == "Bar") && k.Caller == "<file>" {
			// Top-level decl line — should NOT register as a ref.
			if k.Line == 3 || k.Line == 5 {
				t.Errorf("declaring ident registered as ref: %+v", k)
			}
		}
	}
}

func TestExtractRefs_MethodReceiverType(t *testing.T) {
	src := []byte(`package src

type Server struct{}

func (s *Server) Run() {}
`)
	got := ExtractRefs("src/x.go", src)
	// (s *Server) — Server is a type ref inside the receiver field.
	mustHave(t, got, refKey{Caller: "Run", Callee: "Server", Kind: "type"})
}

func TestExtractRefs_SkipParseErrors(t *testing.T) {
	// Unparseable source returns nil, not an error.
	got := ExtractRefs("src/x.go", []byte("package src\nfunc {{{"))
	if got != nil {
		t.Errorf("want nil on parse error, got %v", got)
	}
}
