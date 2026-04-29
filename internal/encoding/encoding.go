// Package encoding implements keeba's pluggable agent-only output formats.
//
// The plan §16 lock (2026-04-28): "wiki output is no longer human-readable.
// Optimized purely for agent-context efficiency. v0.1 ships 4 plugins:
// glossary-dedupe, md-caveman, llmlingua, dense-tuple. Hard cap: don't
// compress past 4× (quality cliff)."
//
// v0.4 ships md-caveman, glossary-dedupe, dense-tuple, and a fifth plugin
// structural-card (function-page extractor — emerged from the
// docs/encoding-bench-2026-04-29.md benches as the top performer on
// cross-file code retrieval at ~3.4× compression). llmlingua remains
// deferred pending the runtime model dependency story.
//
// Every encoder is applied to the body of an imported / ingested page,
// AFTER wrapping with keeba's frontmatter and section structure but
// BEFORE writing to disk. Frontmatter (YAML between `---` delimiters),
// code blocks (fenced ``` and inline `…`), wiki links ([[…]]), and URLs
// are passed through unchanged — encoding only operates on prose.
package encoding

import (
	"fmt"
	"strings"
)

// Encoder is the contract every encoding plugin satisfies. Stateless
// from the caller's perspective; safe to call from any goroutine after
// any required Fit(...) has completed.
type Encoder interface {
	// Name is the stable identifier used in keeba.config.yaml.
	Name() string
	// Encode transforms a wrapped page body into the encoded form. The
	// input includes frontmatter; encoders MUST preserve it verbatim.
	Encode(body string) (string, error)
}

// Fitter is implemented by stateful encoders that need to scan the
// corpus once before encoding (e.g. glossary-dedupe builds canonical
// short codes from frequency analysis). Fit MUST be called before
// Encode for plugins implementing this interface.
type Fitter interface {
	Fit(corpus []string) error
}

// Pass is the no-op encoder ("encoding: none" / "encoding: markdown").
// Used as the default until a user opts into a real plugin via
// keeba.config.yaml.
type Pass struct{}

// Name returns "none" — the canonical identifier for the no-op encoder.
func (Pass) Name() string { return "none" }

// Encode returns body unchanged.
func (Pass) Encode(body string) (string, error) { return body, nil }

// Pipeline chains encoders left-to-right. A Pipeline IS-A Encoder so
// it can be used wherever a single encoder is expected.
type Pipeline struct {
	encoders []Encoder
}

// NewPipeline constructs a Pipeline of the given encoders, applied
// in argument order.
func NewPipeline(encoders ...Encoder) *Pipeline {
	return &Pipeline{encoders: append([]Encoder(nil), encoders...)}
}

// Encoders returns the constituent encoders in order. The returned
// slice is a copy — safe to mutate.
func (p *Pipeline) Encoders() []Encoder {
	out := make([]Encoder, len(p.encoders))
	copy(out, p.encoders)
	return out
}

// Name joins constituent encoder names with "+", or returns "raw"
// for an empty pipeline.
func (p *Pipeline) Name() string {
	if p == nil || len(p.encoders) == 0 {
		return "raw"
	}
	parts := make([]string, len(p.encoders))
	for i, e := range p.encoders {
		parts[i] = e.Name()
	}
	return strings.Join(parts, "+")
}

// Encode runs body through each encoder in order. The first error
// short-circuits and is wrapped with the failing encoder's name.
func (p *Pipeline) Encode(body string) (string, error) {
	if p == nil {
		return body, nil
	}
	var err error
	for _, e := range p.encoders {
		body, err = e.Encode(body)
		if err != nil {
			return body, fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return body, nil
}

// Fit calls Fit on any Fitter encoders, threading the encoded output
// through to subsequent stateful encoders. Plain (non-Fitter) encoders
// are skipped here — they're applied at Encode time.
func (p *Pipeline) Fit(corpus []string) error {
	if p == nil || len(p.encoders) == 0 {
		return nil
	}
	lastFit := -1
	for i, e := range p.encoders {
		if _, ok := e.(Fitter); ok {
			lastFit = i
		}
	}
	if lastFit < 0 {
		return nil
	}

	current := corpus
	for i := 0; i <= lastFit; i++ {
		if f, ok := p.encoders[i].(Fitter); ok {
			if err := f.Fit(current); err != nil {
				return fmt.Errorf("%s: %w", p.encoders[i].Name(), err)
			}
		}
		if i < lastFit {
			next := make([]string, len(current))
			for j, t := range current {
				out, err := p.encoders[i].Encode(t)
				if err != nil {
					return fmt.Errorf("%s: %w", p.encoders[i].Name(), err)
				}
				next[j] = out
			}
			current = next
		}
	}
	return nil
}

// BuildPipeline parses an encoder spec — comma-separated
// (`"glossary,structural-card"`, the canonical CLI form) or plus-joined
// (`"glossary-dedupe+md-caveman"`, the form Pipeline.Name() produces) —
// and constructs the corresponding Pipeline. Both separators are
// accepted so a spec written by `keeba bench --write-config` round-trips
// cleanly when re-read by `keeba ingest` / `keeba sync`. Empty or "raw"
// yields an empty (no-op) pipeline. Unknown encoder names error.
func BuildPipeline(spec string) (*Pipeline, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "raw" {
		return NewPipeline(), nil
	}
	// Normalize "+" (Pipeline.Name output) to "," (canonical input form)
	// so the two are interchangeable end-to-end.
	spec = strings.ReplaceAll(spec, "+", ",")
	parts := strings.Split(spec, ",")
	encs := make([]Encoder, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		e, err := newByName(part)
		if err != nil {
			return nil, err
		}
		encs = append(encs, e)
	}
	return NewPipeline(encs...), nil
}

// ByName returns the encoder for an identifier. Returns Pass{} for
// unknown / empty strings so a missing config value never breaks an
// import. For strict resolution use BuildPipeline.
func ByName(name string) Encoder {
	e, err := newByName(strings.TrimSpace(name))
	if err != nil {
		return Pass{}
	}
	return e
}

func newByName(name string) (Encoder, error) {
	switch name {
	case "", "none", "markdown", "raw":
		return Pass{}, nil
	case "md-caveman", "caveman":
		return MDCaveman{}, nil
	case "glossary-dedupe", "glossary":
		return NewGlossary(), nil
	case "structural-card", "structural_card", "card":
		return StructuralCard{}, nil
	case "dense-tuple", "dense_tuple":
		return DenseTuple{}, nil
	}
	return nil, fmt.Errorf("encoding: unknown encoder %q", name)
}
