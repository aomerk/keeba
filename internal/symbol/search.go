package symbol

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// SearchHit is one ranked symbol from BM25Index.Query. Score is the
// raw BM25 value — higher is more relevant. Used by the search_symbols
// MCP tool so the agent can do "what handles auth?" before knowing any
// exact symbol names.
type SearchHit struct {
	Symbol Symbol  `json:"symbol"`
	Score  float64 `json:"score"`
}

// BM25Index is an in-memory BM25 index over the symbol set. The
// "document" for each symbol is its name + signature + doc concatenated;
// queries are tokenized through the same identifier-aware splitter that
// the encoding plugins use, so "auth" matches "AuthMiddleware" and
// "Validate" matches "validate_token".
type BM25Index struct {
	syms     []Symbol
	tokens   [][]string // per-symbol tokens (parallel to syms)
	tfCounts []map[string]int
	df       map[string]int
	totalLen int
	avgdl    float64
}

// BuildBM25Index tokenizes each symbol once and builds the
// document-frequency map BM25 needs. O(N) over symbol bytes; on a
// 10k-symbol repo this is sub-100ms.
func BuildBM25Index(syms []Symbol) *BM25Index {
	idx := &BM25Index{
		syms:     syms,
		tokens:   make([][]string, len(syms)),
		tfCounts: make([]map[string]int, len(syms)),
		df:       make(map[string]int),
	}
	totalLen := 0
	for i, s := range syms {
		toks := symbolTokens(s)
		idx.tokens[i] = toks
		tf := make(map[string]int, len(toks))
		for _, t := range toks {
			tf[t]++
		}
		idx.tfCounts[i] = tf
		for term := range tf {
			idx.df[term]++
		}
		totalLen += len(toks)
	}
	idx.totalLen = totalLen
	if n := len(syms); n > 0 {
		idx.avgdl = float64(totalLen) / float64(n)
	}
	return idx
}

// symbolTokens returns the BM25 token bag for one symbol. Includes
// camelCase / snake_case splits of the name + signature identifiers,
// plus tokenized doc text. Doc tokens go through the lighter prose
// tokenizer (lowercase + word-split) so "auth flow" matches both the
// signature and the doc comment of an AuthMiddleware function.
func symbolTokens(s Symbol) []string {
	var out []string
	out = append(out, splitIdent(s.Name)...)
	if s.Receiver != "" {
		out = append(out, splitIdent(s.Receiver)...)
	}
	out = append(out, identifiersInText(s.Signature)...)
	out = append(out, proseTokens(s.Doc)...)
	return out
}

// identInTextRE pulls every identifier-shaped substring from a
// signature or other code-shaped string.
var identInTextRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// splitIdent breaks "AuthMiddleware" → ["auth", "middleware", "authmiddleware"]
// and "auth_token_verifier" → ["auth", "token", "verifier", "auth_token_verifier"].
// "JSONParser" → ["json", "parser", "jsonparser"]: an upper-run followed by a
// title-cased word splits at the last upper letter (RE2 has no lookahead, so we
// walk runes manually). The bare lowercase identifier is appended too so
// exact-name queries still rank highly.
func splitIdent(name string) []string {
	if name == "" {
		return nil
	}
	parts := []string{}
	for _, chunk := range strings.Split(name, "_") {
		if chunk == "" {
			continue
		}
		rs := []rune(chunk)
		start := 0
		for i := 1; i < len(rs); i++ {
			prev, cur := rs[i-1], rs[i]
			split := false
			switch {
			case isLower(prev) && isUpper(cur): // foo|Bar
				split = true
			case isUpper(prev) && isUpper(cur) && i+1 < len(rs) && isLower(rs[i+1]):
				split = true // JSON|Parser
			case isDigit(prev) != isDigit(cur):
				split = true // foo|2 / 2|foo
			}
			if split {
				parts = append(parts, strings.ToLower(string(rs[start:i])))
				start = i
			}
		}
		parts = append(parts, strings.ToLower(string(rs[start:])))
	}
	parts = append(parts, strings.ToLower(name))
	return parts
}

func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// identifiersInText runs splitIdent over every identifier-shaped
// substring in text — typically a signature line.
func identifiersInText(text string) []string {
	var out []string
	for _, m := range identInTextRE.FindAllString(text, -1) {
		out = append(out, splitIdent(m)...)
	}
	return out
}

// proseStopwords is the same trimmed list as internal/search uses; kept
// inline so the symbol package doesn't depend on the wiki search pkg.
var proseStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {},
	"be": {}, "by": {}, "for": {}, "from": {}, "if": {}, "in": {},
	"into": {}, "is": {}, "it": {}, "its": {}, "of": {}, "on": {},
	"or": {}, "that": {}, "the": {}, "this": {}, "to": {}, "was": {},
	"with": {}, "you": {}, "your": {}, "have": {}, "has": {}, "had": {},
}

// proseTokens splits doc text on non-alphanumeric runs, lowercases,
// drops short / stopword tokens.
func proseTokens(text string) []string {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !isAlphaNum(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		if _, drop := proseStopwords[f]; drop {
			continue
		}
		out = append(out, f)
	}
	return out
}

func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// Query returns the top-k highest-scoring symbols. Ties broken by
// (file, line) for stable output across runs.
func (i *BM25Index) Query(q string, k int) []SearchHit {
	if i == nil || len(i.syms) == 0 || k <= 0 {
		return nil
	}
	terms := queryTokens(q)
	if len(terms) == 0 {
		return nil
	}

	const k1 = 1.5
	const bp = 0.75
	totalN := float64(len(i.syms))

	type scored struct {
		idx   int
		score float64
	}
	out := make([]scored, 0, len(i.syms))
	for di := range i.syms {
		score := 0.0
		dl := float64(len(i.tokens[di]))
		tf := i.tfCounts[di]
		for _, t := range terms {
			f := float64(tf[t])
			if f == 0 {
				continue
			}
			n := float64(i.df[t])
			idf := math.Log((totalN-n+0.5)/(n+0.5) + 1)
			norm := f * (k1 + 1) / (f + k1*(1-bp+bp*dl/i.avgdl))
			score += idf * norm
		}
		if score > 0 {
			out = append(out, scored{idx: di, score: score})
		}
	}

	sort.Slice(out, func(a, bj int) bool {
		if out[a].score != out[bj].score {
			return out[a].score > out[bj].score
		}
		// Tie-break on file/line so same-score hits diff cleanly.
		sa, sb := i.syms[out[a].idx], i.syms[out[bj].idx]
		if sa.File != sb.File {
			return sa.File < sb.File
		}
		return sa.StartLine < sb.StartLine
	})

	if len(out) == 0 {
		return nil
	}
	if k > len(out) {
		k = len(out)
	}
	hits := make([]SearchHit, 0, k)
	for _, s := range out[:k] {
		hits = append(hits, SearchHit{Symbol: i.syms[s.idx], Score: s.score})
	}
	return hits
}

// queryTokens runs both identifier-aware and prose tokenization on the
// query so "auth middleware" and "AuthMiddleware" both match the right
// symbols. Bare and split forms are merged into one token bag.
func queryTokens(q string) []string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	out := []string{}
	for _, m := range identInTextRE.FindAllString(q, -1) {
		out = append(out, splitIdent(m)...)
	}
	out = append(out, proseTokens(q)...)

	// Dedupe in place — preserves order, drops duplicates.
	seen := map[string]struct{}{}
	uniq := out[:0]
	for _, t := range out {
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		uniq = append(uniq, t)
	}
	return uniq
}
