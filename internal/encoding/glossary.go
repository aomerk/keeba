package encoding

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	glossaryDefaultMinFreq    = 8
	glossaryDefaultMaxEntries = 256
	glossaryDefaultPrefix     = "g"
	// minimum token length to alias — shorter tokens rarely save bytes
	// after the rewrite + glossary preamble cost.
	glossaryMinIdentLen = 8
)

// Identifier candidates: alphanumeric_/snake_case, length >= 7.
// Length is enforced by the regex to avoid scanning every short token;
// the final length check uses glossaryMinIdentLen above to allow tuning.
var glossaryIdentRE = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]{6,}\b`)

// GlossaryDedupe scans the corpus for frequent long identifiers and
// rewrites them to short canonical codes (g0, g1, …). Saves bytes both
// in the index AND in agent-context (the glossary preamble loads once
// per session vs repeating long identifiers across N pages).
type GlossaryDedupe struct {
	MinFreq    int
	MaxEntries int
	Prefix     string

	mu      sync.RWMutex
	table   map[string]string
	pattern *regexp.Regexp
}

// NewGlossary returns a GlossaryDedupe with default tuning. Override
// the exported fields before calling Fit if needed.
func NewGlossary() *GlossaryDedupe {
	return &GlossaryDedupe{
		MinFreq:    glossaryDefaultMinFreq,
		MaxEntries: glossaryDefaultMaxEntries,
		Prefix:     glossaryDefaultPrefix,
	}
}

// Name returns "glossary-dedupe".
func (*GlossaryDedupe) Name() string { return "glossary-dedupe" }

// Glossary returns a copy of the (token -> code) table. Callers can
// emit it as a session-time preamble for agents.
func (g *GlossaryDedupe) Glossary() map[string]string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]string, len(g.table))
	for k, v := range g.table {
		out[k] = v
	}
	return out
}

type glossaryCandidate struct {
	tok string
	n   int
}

// Fit scans the corpus, picks the top-N long identifiers by
// frequency-weighted byte savings, and assigns canonical short codes.
// Re-running Fit replaces the table.
func (g *GlossaryDedupe) Fit(corpus []string) error {
	minFreq := g.MinFreq
	if minFreq <= 0 {
		minFreq = glossaryDefaultMinFreq
	}
	maxEntries := g.MaxEntries
	if maxEntries <= 0 {
		maxEntries = glossaryDefaultMaxEntries
	}
	prefix := g.Prefix
	if prefix == "" {
		prefix = glossaryDefaultPrefix
	}

	counts := make(map[string]int)
	for _, text := range corpus {
		for _, m := range glossaryIdentRE.FindAllString(text, -1) {
			counts[m]++
		}
	}

	candidates := make([]glossaryCandidate, 0, len(counts))
	for tok, n := range counts {
		if n >= minFreq && len(tok) >= glossaryMinIdentLen {
			candidates = append(candidates, glossaryCandidate{tok, n})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		wi := candidates[i].n * len(candidates[i].tok)
		wj := candidates[j].n * len(candidates[j].tok)
		if wi != wj {
			return wi > wj
		}
		return candidates[i].tok < candidates[j].tok
	})
	if len(candidates) > maxEntries {
		candidates = candidates[:maxEntries]
	}

	table := make(map[string]string, len(candidates))
	for i, c := range candidates {
		table[c.tok] = prefix + strconv.Itoa(i)
	}

	var pattern *regexp.Regexp
	if len(table) > 0 {
		keys := make([]string, 0, len(table))
		for k := range table {
			keys = append(keys, k)
		}
		// longest-first to avoid prefix-of-other-token collisions
		sort.Slice(keys, func(i, j int) bool {
			if len(keys[i]) != len(keys[j]) {
				return len(keys[i]) > len(keys[j])
			}
			return keys[i] < keys[j]
		})
		escaped := make([]string, len(keys))
		for i, k := range keys {
			escaped[i] = regexp.QuoteMeta(k)
		}
		pattern = regexp.MustCompile(`\b(` + strings.Join(escaped, "|") + `)\b`)
	}

	g.mu.Lock()
	g.table = table
	g.pattern = pattern
	g.mu.Unlock()
	return nil
}

// Encode rewrites every glossaried identifier in body to its short code.
// Returns body unchanged if Fit hasn't been called or produced no
// entries (i.e. corpus too small for any identifier to clear MinFreq).
func (g *GlossaryDedupe) Encode(body string) (string, error) {
	g.mu.RLock()
	pat := g.pattern
	table := g.table
	g.mu.RUnlock()
	if pat == nil {
		return body, nil
	}
	return pat.ReplaceAllStringFunc(body, func(s string) string {
		if v, ok := table[s]; ok {
			return v
		}
		return s
	}), nil
}
