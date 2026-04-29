package encoding

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	cardDefaultTopIdents = 30
	cardDefaultDocChars  = 160
)

var (
	cardTokenRE     = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	cardDocstringRE = regexp.MustCompile(`(?s)("""|''')\s*([^"']+?)([.\n]|"""|''')`)
)

var cardSigPrefixes = []string{
	"def ", "class ", "function ", "func ", "fn ", "async def ",
}

// StructuralCard is a function-page encoder. Extracts: signature, first
// docstring sentence, top identifiers by frequency. Targets the
// "function" page type — discards prose narrative. ~3.5-4× compression.
//
// Empirical (docs/encoding-bench-2026-04-29.md):
//   - CSN Python (200 q, 22k corpus): MRR 0.925 vs BM25 0.918 (+0.7%) at 3.95×
//   - RepoBench-R Python (500 q, 1.8k corpus): MRR 0.521 vs BM25 0.249 (+109%) at 3.21×
//
// Right at the LLMLingua-2 quality cliff but on the safe side. Apply
// only to function / class pages; on entity-fact pages prefer
// dense-tuple, on narrative prose prefer glossary+md-caveman.
type StructuralCard struct {
	TopIdents int
	DocChars  int
}

// Name returns "structural-card".
func (StructuralCard) Name() string { return "structural-card" }

type cardCandidate struct {
	tok string
	n   int
}

// Encode emits "<signature> <doc-line> <top-N idents>" — the highest-
// signal slice of a definition, dropping bodies and boilerplate.
func (s StructuralCard) Encode(body string) (string, error) {
	topN := s.TopIdents
	if topN <= 0 {
		topN = cardDefaultTopIdents
	}
	docMax := s.DocChars
	if docMax <= 0 {
		docMax = cardDefaultDocChars
	}

	sig := extractSignature(body)
	doc := extractDocLine(body, docMax)
	top := topIdentifiers(body, topN)

	parts := make([]string, 0, 3)
	if sig != "" {
		parts = append(parts, sig)
	}
	if doc != "" {
		parts = append(parts, doc)
	}
	if len(top) > 0 {
		parts = append(parts, strings.Join(top, " "))
	}
	return strings.Join(parts, " "), nil
}

func extractSignature(body string) string {
	for _, line := range strings.Split(body, "\n") {
		ts := strings.TrimSpace(line)
		for _, p := range cardSigPrefixes {
			if strings.HasPrefix(ts, p) {
				return ts
			}
		}
	}
	if i := strings.IndexByte(body, '\n'); i > 0 {
		return strings.TrimSpace(body[:i])
	}
	return strings.TrimSpace(body)
}

func extractDocLine(body string, max int) string {
	m := cardDocstringRE.FindStringSubmatch(body)
	if len(m) < 3 {
		return ""
	}
	candidate := strings.TrimSpace(m[2])
	if candidate == "" {
		return ""
	}
	if i := strings.IndexByte(candidate, '\n'); i >= 0 {
		candidate = candidate[:i]
	}
	if utf8.RuneCountInString(candidate) > max {
		// safe truncation on rune boundary
		runes := []rune(candidate)
		if len(runes) > max {
			candidate = string(runes[:max])
		}
	}
	return candidate
}

func topIdentifiers(body string, n int) []string {
	counts := make(map[string]int)
	for _, m := range cardTokenRE.FindAllString(body, -1) {
		counts[m]++
	}
	keys := make([]cardCandidate, 0, len(counts))
	for k, v := range counts {
		keys = append(keys, cardCandidate{k, v})
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].n != keys[j].n {
			return keys[i].n > keys[j].n
		}
		return keys[i].tok < keys[j].tok
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	out := make([]string, len(keys))
	for i, e := range keys {
		out[i] = e.tok
	}
	return out
}
