package encoding

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const denseTupleDefaultTopIdents = 12

var (
	denseDefPyRE = regexp.MustCompile(
		`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(([^)]*)\)(?:\s*->\s*([^:]+?))?\s*:`,
	)
	denseDefJSRE = regexp.MustCompile(
		`(?m)^\s*(?:async\s+)?function\s+([A-Za-z_]\w*)\s*\(([^)]*)\)`,
	)
	denseDefGoRE = regexp.MustCompile(
		`(?m)^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(([^)]*)\)\s*([^{]*)\{`,
	)
	denseTokenRE = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]{2,}\b`)
)

var denseTupleStops = map[string]struct{}{
	"self": {}, "cls": {}, "None": {}, "True": {}, "False": {},
	"def": {}, "class": {}, "return": {}, "if": {}, "else": {},
	"for": {}, "while": {}, "import": {}, "from": {}, "as": {},
	"in": {}, "is": {}, "not": {}, "and": {}, "or": {},
}

// DenseTuple emits RDF-style (subject, predicate, object) facts about a
// definition: name, params, return type, body identifiers. Designed for
// fact-heavy entity pages.
//
// Empirical (docs/encoding-bench-2026-04-29.md): on function corpora
// (CSN Python) it underperforms structural-card by ~13% MRR — applying
// the wrong page-type encoder costs quality. Use for entity / fact
// pages per plan §10 page-type-aware selector.
type DenseTuple struct {
	TopIdents int
}

// Name returns "dense-tuple".
func (DenseTuple) Name() string { return "dense-tuple" }

type denseDef struct {
	name      string
	paramsRaw string
	rtype     string
}

func extractDef(body string) denseDef {
	for _, re := range []*regexp.Regexp{denseDefPyRE, denseDefJSRE, denseDefGoRE} {
		m := re.FindStringSubmatch(body)
		if len(m) >= 3 {
			rtype := ""
			if len(m) >= 4 {
				rtype = strings.TrimSpace(m[3])
			}
			return denseDef{name: m[1], paramsRaw: m[2], rtype: rtype}
		}
	}
	return denseDef{name: "_"}
}

func splitParams(raw string) []string {
	out := []string{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if i := strings.IndexAny(p, ":="); i > 0 {
			p = strings.TrimSpace(p[:i])
		}
		if p == "" || p == "self" || p == "cls" || p == "*" || p == "/" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Encode produces a flat triple-stream like
// "fname name fname. fname param x. fname returns int. fname uses bar.".
func (d DenseTuple) Encode(body string) (string, error) {
	topN := d.TopIdents
	if topN <= 0 {
		topN = denseTupleDefaultTopIdents
	}

	def := extractDef(body)
	params := splitParams(def.paramsRaw)

	counts := make(map[string]int)
	for _, m := range denseTokenRE.FindAllString(body, -1) {
		if _, stop := denseTupleStops[m]; stop {
			continue
		}
		if m == def.name {
			continue
		}
		counts[m]++
	}
	type kv struct {
		k string
		n int
	}
	keys := make([]kv, 0, len(counts))
	for k, n := range counts {
		keys = append(keys, kv{k, n})
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].n != keys[j].n {
			return keys[i].n > keys[j].n
		}
		return keys[i].k < keys[j].k
	})
	if len(keys) > topN {
		keys = keys[:topN]
	}

	triples := []string{fmt.Sprintf("%s name %s", def.name, def.name)}
	for _, p := range params {
		triples = append(triples, fmt.Sprintf("%s param %s", def.name, p))
	}
	if def.rtype != "" {
		triples = append(triples, fmt.Sprintf("%s returns %s", def.name, def.rtype))
	}
	for _, e := range keys {
		triples = append(triples, fmt.Sprintf("%s uses %s", def.name, e.k))
	}

	return strings.Join(triples, ". ") + ".", nil
}
