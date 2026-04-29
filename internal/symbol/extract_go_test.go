package symbol

import (
	"strings"
	"testing"
)

func TestGoExtractor_FunctionsAndMethods(t *testing.T) {
	src := []byte(`package foo

import "fmt"

// Greet prints hello.
func Greet(name string) error {
	fmt.Println("hi", name)
	return nil
}

type Server struct {
	addr string
}

func (s *Server) Start() error { return nil }

func (s Server) Addr() string { return s.addr }
`)
	syms, err := goExtractor{}.Extract("foo.go", src)
	if err != nil {
		t.Fatalf("Extract err = %v", err)
	}

	want := map[string]Symbol{
		"Greet": {
			Name: "Greet", Kind: "function", File: "foo.go",
			// Pos is the `func` keyword line (line 6); the doc comment
			// on line 5 is captured into Symbol.Doc separately.
			StartLine: 6, Receiver: "", Language: "go",
		},
		"Server": {
			Name: "Server", Kind: "type", File: "foo.go",
			StartLine: 11, Language: "go",
		},
		"Start": {
			Name: "Start", Kind: "method", File: "foo.go",
			StartLine: 15, Receiver: "Server", Language: "go",
		},
		"Addr": {
			Name: "Addr", Kind: "method", File: "foo.go",
			StartLine: 17, Receiver: "Server", Language: "go",
		},
	}

	if len(syms) != len(want) {
		t.Errorf("got %d symbols, want %d: %+v", len(syms), len(want), syms)
	}
	gotByName := map[string]Symbol{}
	for _, s := range syms {
		gotByName[s.Name] = s
	}
	for name, w := range want {
		got, ok := gotByName[name]
		if !ok {
			t.Errorf("missing symbol %q", name)
			continue
		}
		if got.Kind != w.Kind {
			t.Errorf("%s.Kind = %q, want %q", name, got.Kind, w.Kind)
		}
		if got.Receiver != w.Receiver {
			t.Errorf("%s.Receiver = %q, want %q", name, got.Receiver, w.Receiver)
		}
		if got.StartLine != w.StartLine {
			t.Errorf("%s.StartLine = %d, want %d", name, got.StartLine, w.StartLine)
		}
		if got.Language != w.Language {
			t.Errorf("%s.Language = %q, want %q", name, got.Language, w.Language)
		}
	}
}

func TestGoExtractor_DocAttachedToFunction(t *testing.T) {
	src := []byte(`package foo

// Greet prints hello.
// Multi-line comment.
func Greet() {}
`)
	syms, err := goExtractor{}.Extract("foo.go", src)
	if err != nil || len(syms) == 0 {
		t.Fatalf("Extract err = %v, syms = %v", err, syms)
	}
	if !strings.Contains(syms[0].Doc, "Greet prints hello") {
		t.Errorf("Doc didn't capture comment, got %q", syms[0].Doc)
	}
}

func TestGoExtractor_GenericTypeReceiver(t *testing.T) {
	src := []byte(`package foo

type Bag[T any] struct{}

func (b *Bag[T]) Add(item T) {}
`)
	syms, err := goExtractor{}.Extract("foo.go", src)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var add *Symbol
	for i, s := range syms {
		if s.Name == "Add" {
			add = &syms[i]
			break
		}
	}
	if add == nil {
		t.Fatal("Add method missing")
	}
	if add.Receiver != "Bag" {
		t.Errorf("generic receiver = %q, want Bag", add.Receiver)
	}
}

func TestGoExtractor_ExportedConstAndVar(t *testing.T) {
	src := []byte(`package foo

const PublicMax = 10
const privateMax = 5

var ExportedSet = map[string]int{}
var unexportedSet = map[string]int{}
`)
	syms, err := goExtractor{}.Extract("foo.go", src)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	names := map[string]string{}
	for _, s := range syms {
		names[s.Name] = s.Kind
	}
	if names["PublicMax"] != "const" {
		t.Errorf("PublicMax kind = %q, want const", names["PublicMax"])
	}
	if names["ExportedSet"] != "var" {
		t.Errorf("ExportedSet kind = %q, want var", names["ExportedSet"])
	}
	if _, ok := names["privateMax"]; ok {
		t.Errorf("unexported privateMax should be skipped")
	}
	if _, ok := names["unexportedSet"]; ok {
		t.Errorf("unexported unexportedSet should be skipped")
	}
}

func TestGoExtractor_ParseErrorReturnsErr(t *testing.T) {
	src := []byte(`package foo

func broken( {
`)
	_, err := goExtractor{}.Extract("foo.go", src)
	if err == nil {
		t.Error("expected parse error on broken source")
	}
}
