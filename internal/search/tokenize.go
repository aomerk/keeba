package search

import (
	"strings"
	"unicode"
)

// stopwords is a small English stopword list. Tokens in this set are dropped
// before indexing so common chatter doesn't inflate document frequency.
var stopwords = map[string]struct{}{
	"a": {}, "am": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {},
	"be": {}, "but": {}, "by": {}, "for": {}, "from": {}, "had": {},
	"has": {}, "have": {}, "he": {}, "her": {}, "him": {}, "his": {},
	"if": {}, "in": {}, "into": {}, "is": {}, "it": {}, "its": {},
	"of": {}, "on": {}, "or": {}, "she": {}, "so": {}, "that": {},
	"the": {}, "their": {}, "them": {}, "then": {}, "there": {},
	"these": {}, "they": {}, "this": {}, "to": {}, "was": {}, "we": {},
	"were": {}, "what": {}, "when": {}, "which": {}, "who": {}, "why": {},
	"will": {}, "with": {}, "would": {}, "you": {}, "your": {},
}

// Tokenize lowercases the input, splits on non-letter/digit runs, drops
// stopwords + tokens shorter than 2 chars, and returns the survivors. Pure
// stdlib; no stemming in v0.1 to keep the index deterministic and tiny.
func Tokenize(text string) []string {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		if _, drop := stopwords[f]; drop {
			continue
		}
		out = append(out, f)
	}
	return out
}
