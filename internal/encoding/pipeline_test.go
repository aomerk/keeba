package encoding

import (
	"errors"
	"strings"
	"testing"
)

type fakeFitterEnc struct {
	fitCalls int
	fitArgs  [][]string
	prefix   string
}

func (*fakeFitterEnc) Name() string { return "fake-fitter" }

func (f *fakeFitterEnc) Fit(corpus []string) error {
	f.fitCalls++
	cp := make([]string, len(corpus))
	copy(cp, corpus)
	f.fitArgs = append(f.fitArgs, cp)
	return nil
}

func (f *fakeFitterEnc) Encode(body string) (string, error) {
	return f.prefix + body, nil
}

type errEnc struct{}

func (errEnc) Name() string                  { return "boom" }
func (errEnc) Encode(string) (string, error) { return "", errors.New("kaboom") }

func TestPipelineEmptyName(t *testing.T) {
	if got := NewPipeline().Name(); got != "raw" {
		t.Errorf("empty pipeline Name = %q, want %q", got, "raw")
	}
}

func TestPipelineNameJoinsWithPlus(t *testing.T) {
	p := NewPipeline(MDCaveman{}, StructuralCard{})
	if got := p.Name(); got != "md-caveman+structural-card" {
		t.Errorf("Name = %q", got)
	}
}

func TestPipelineEncodeChains(t *testing.T) {
	p := NewPipeline(
		&fakeFitterEnc{prefix: "A:"},
		&fakeFitterEnc{prefix: "B:"},
	)
	got, err := p.Encode("body")
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if got != "B:A:body" {
		t.Errorf("Encode = %q, want %q", got, "B:A:body")
	}
}

func TestPipelineEncodeWrapsErrors(t *testing.T) {
	p := NewPipeline(errEnc{})
	_, err := p.Encode("body")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected encoder name in error, got %v", err)
	}
}

func TestPipelineFitOnlyStateful(t *testing.T) {
	stateful := &fakeFitterEnc{prefix: "X:"}
	p := NewPipeline(MDCaveman{}, stateful)
	if err := p.Fit([]string{"a", "b"}); err != nil {
		t.Fatalf("Fit err = %v", err)
	}
	if stateful.fitCalls != 1 {
		t.Errorf("expected 1 fit call, got %d", stateful.fitCalls)
	}
}

func TestPipelineFitThreadsThroughEarlierEncoders(t *testing.T) {
	a := &fakeFitterEnc{prefix: "A:"}
	b := &fakeFitterEnc{prefix: "B:"}
	p := NewPipeline(a, b)
	if err := p.Fit([]string{"x", "y"}); err != nil {
		t.Fatalf("Fit err = %v", err)
	}
	// b should see A's encoded output
	if len(b.fitArgs) == 0 || b.fitArgs[0][0] != "A:x" {
		t.Errorf("b.Fit should see A's output, got %v", b.fitArgs)
	}
}

func TestPipelineFitNoopWhenNoFitter(t *testing.T) {
	p := NewPipeline(MDCaveman{}, StructuralCard{})
	if err := p.Fit([]string{"text"}); err != nil {
		t.Errorf("Fit on stateless pipeline should not error: %v", err)
	}
}

func TestBuildPipeline(t *testing.T) {
	cases := []struct {
		name  string
		spec  string
		want  string
		isErr bool
	}{
		{"empty", "", "raw", false},
		{"raw alias", "raw", "raw", false},
		{"single", "md-caveman", "md-caveman", false},
		{"caveman alias", "caveman", "md-caveman", false},
		{"glossary alias", "glossary", "glossary-dedupe", false},
		{"card alias", "card", "structural-card", false},
		{"chain", "glossary,structural-card", "glossary-dedupe+structural-card", false},
		{"plus separator round-trip", "glossary-dedupe+md-caveman", "glossary-dedupe+md-caveman", false},
		{"mixed separators", "glossary+md-caveman,structural-card", "glossary-dedupe+md-caveman+structural-card", false},
		{"unknown", "wat", "", true},
		{"trims spaces", "  glossary , caveman  ", "glossary-dedupe+md-caveman", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := BuildPipeline(tc.spec)
			if tc.isErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tc.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildPipeline(%q) err = %v", tc.spec, err)
			}
			if got := p.Name(); got != tc.want {
				t.Errorf("BuildPipeline(%q).Name() = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}

func TestByNameUnknownReturnsPass(t *testing.T) {
	enc := ByName("does-not-exist")
	if _, ok := enc.(Pass); !ok {
		t.Errorf("ByName(unknown) should return Pass, got %T", enc)
	}
}

func TestByNameKnown(t *testing.T) {
	cases := map[string]string{
		"md-caveman":      "md-caveman",
		"glossary":        "glossary-dedupe",
		"structural-card": "structural-card",
		"dense-tuple":     "dense-tuple",
	}
	for spec, want := range cases {
		t.Run(spec, func(t *testing.T) {
			if got := ByName(spec).Name(); got != want {
				t.Errorf("ByName(%q).Name() = %q, want %q", spec, got, want)
			}
		})
	}
}
