package encoding

import (
	"strings"
	"testing"
)

func TestDenseTupleName(t *testing.T) {
	if got := (DenseTuple{}).Name(); got != "dense-tuple" {
		t.Errorf("Name() = %q, want %q", got, "dense-tuple")
	}
}

func TestDenseTuplePython(t *testing.T) {
	body := `def add(x: int, y: int) -> int:
    return x + y
`
	got, err := DenseTuple{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	for _, want := range []string{
		"add name add",
		"add param x",
		"add param y",
		"add returns int",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected triple %q in output, got %q", want, got)
		}
	}
}

func TestDenseTupleGo(t *testing.T) {
	body := `func RegisterHandler(name string, fn HandlerFunc) error {
	registry[name] = fn
	return nil
}`
	got, err := DenseTuple{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if !strings.Contains(got, "RegisterHandler name RegisterHandler") {
		t.Errorf("expected name triple in output, got %q", got)
	}
	if !strings.Contains(got, "RegisterHandler param name") {
		t.Errorf("expected param triple in output, got %q", got)
	}
}

func TestDenseTupleJS(t *testing.T) {
	body := `function buildPath(parts, sep) {
  return parts.join(sep);
}`
	got, err := DenseTuple{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if !strings.Contains(got, "buildPath name buildPath") {
		t.Errorf("expected name triple, got %q", got)
	}
	if !strings.Contains(got, "buildPath param parts") {
		t.Errorf("expected param triple, got %q", got)
	}
	if !strings.Contains(got, "buildPath param sep") {
		t.Errorf("expected param triple, got %q", got)
	}
}

func TestDenseTupleSkipsSelf(t *testing.T) {
	body := `def method(self, other):
    return self.x + other.x
`
	got, err := DenseTuple{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if strings.Contains(got, "param self") {
		t.Errorf("self should be skipped, got %q", got)
	}
	if !strings.Contains(got, "method param other") {
		t.Errorf("expected other param triple, got %q", got)
	}
}

func TestDenseTupleNoDefFallsBack(t *testing.T) {
	body := "loose text with foo bar baz baz baz"
	got, err := DenseTuple{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	// fname falls back to "_" — at minimum we get the name triple plus uses
	if !strings.Contains(got, "_ name _") {
		t.Errorf("expected fallback name triple, got %q", got)
	}
}

func TestDenseTupleStripsParamAnnotation(t *testing.T) {
	body := `def f(x: SomeType = default_value):
    return x
`
	got, err := DenseTuple{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if !strings.Contains(got, "f param x") {
		t.Errorf("expected stripped param x, got %q", got)
	}
	if strings.Contains(got, "param x:") || strings.Contains(got, "param x =") {
		t.Errorf("annotation should be stripped from param name, got %q", got)
	}
}
