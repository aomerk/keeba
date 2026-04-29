package encoding

import (
	"strings"
	"testing"
)

func TestGlossaryName(t *testing.T) {
	if got := NewGlossary().Name(); got != "glossary-dedupe" {
		t.Errorf("Name() = %q, want %q", got, "glossary-dedupe")
	}
}

func TestGlossaryEncodeWithoutFitIsNoop(t *testing.T) {
	g := NewGlossary()
	in := "longIdentifierName appears here longIdentifierName again"
	got, err := g.Encode(in)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if got != in {
		t.Errorf("Encode without Fit should be no-op, got %q", got)
	}
}

func TestGlossaryFitAndEncode(t *testing.T) {
	g := NewGlossary()
	g.MinFreq = 2 // small corpus
	corpus := []string{
		"longIdentifierName foo",
		"longIdentifierName bar",
		"longIdentifierName baz",
		"unrelatedToken once",
	}
	if err := g.Fit(corpus); err != nil {
		t.Fatalf("Fit err = %v", err)
	}

	table := g.Glossary()
	if _, ok := table["longIdentifierName"]; !ok {
		t.Errorf("expected longIdentifierName in glossary, got %v", table)
	}
	if _, ok := table["unrelatedToken"]; ok {
		t.Errorf("unrelatedToken (freq 1) should not be in glossary")
	}

	got, err := g.Encode("longIdentifierName plus longIdentifierName")
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	code := table["longIdentifierName"]
	want := code + " plus " + code
	if got != want {
		t.Errorf("Encode = %q, want %q", got, want)
	}
}

func TestGlossarySkipsShortIdents(t *testing.T) {
	g := NewGlossary()
	g.MinFreq = 2
	corpus := []string{"abc abc abc abc", "xyz xyz xyz xyz"}
	if err := g.Fit(corpus); err != nil {
		t.Fatalf("Fit err = %v", err)
	}
	if len(g.Glossary()) != 0 {
		t.Errorf("short identifiers (< 8 chars) should not be aliased, got %v", g.Glossary())
	}
}

func TestGlossaryRespectsMaxEntries(t *testing.T) {
	g := NewGlossary()
	g.MinFreq = 2
	g.MaxEntries = 3

	parts := []string{}
	for i := 0; i < 10; i++ {
		// 10 distinct long identifiers each appearing 3 times
		parts = append(parts, "ident_long_x_"+stringMul("z", i+1)+" ")
		parts = append(parts, "ident_long_x_"+stringMul("z", i+1)+" ")
		parts = append(parts, "ident_long_x_"+stringMul("z", i+1))
	}
	if err := g.Fit([]string{strings.Join(parts, "\n")}); err != nil {
		t.Fatalf("Fit err = %v", err)
	}
	if got := len(g.Glossary()); got != 3 {
		t.Errorf("expected 3 entries (MaxEntries cap), got %d", got)
	}
}

func TestGlossaryReFitReplacesTable(t *testing.T) {
	g := NewGlossary()
	g.MinFreq = 2

	if err := g.Fit([]string{"firstIdentifier firstIdentifier"}); err != nil {
		t.Fatalf("Fit err = %v", err)
	}
	if _, ok := g.Glossary()["firstIdentifier"]; !ok {
		t.Fatal("first Fit should have aliased firstIdentifier")
	}

	if err := g.Fit([]string{"secondIdentifier secondIdentifier"}); err != nil {
		t.Fatalf("Fit err = %v", err)
	}
	tab := g.Glossary()
	if _, ok := tab["firstIdentifier"]; ok {
		t.Errorf("re-Fit should clear old entries, got %v", tab)
	}
	if _, ok := tab["secondIdentifier"]; !ok {
		t.Errorf("re-Fit should pick up new corpus, got %v", tab)
	}
}

func stringMul(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
