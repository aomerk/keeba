// Package context turns a natural-language prompt into a markdown
// context block backed by the symbol graph. The CLI's day-1 demo —
// works without MCP integration, paste the output as the first message
// to any AI tool. Closes the agent-loop gap that MCP-only keeba leaves
// open: even when keeba is registered, Claude Code (and friends)
// dispatch to sub-agents that don't inherit MCP, falling back to
// Read/Grep. Context CLI sidesteps the integration entirely.
package context

import (
	"regexp"
	"strings"
)

// commonWords is a small stoplist of English words and CLI noise that
// look like identifiers if you squint but never resolve to a symbol.
// Avoids spamming find_def with "I", "the", "go", etc.
var commonWords = map[string]struct{}{
	"i": {}, "im": {}, "me": {}, "my": {}, "we": {}, "us": {}, "you": {}, "your": {},
	"a": {}, "an": {}, "the": {}, "this": {}, "that": {}, "these": {}, "those": {},
	"is": {}, "was": {}, "be": {}, "been": {}, "being": {}, "are": {}, "were": {},
	"do": {}, "does": {}, "did": {}, "have": {}, "has": {}, "had": {},
	"can": {}, "could": {}, "should": {}, "would": {}, "will": {}, "may": {}, "might": {},
	"and": {}, "or": {}, "but": {}, "if": {}, "then": {}, "else": {}, "when": {}, "while": {},
	"for": {}, "of": {}, "in": {}, "on": {}, "at": {}, "by": {}, "from": {}, "to": {},
	"with": {}, "without": {}, "into": {}, "onto": {}, "as": {}, "than": {},
	"why": {}, "how": {}, "what": {}, "which": {}, "who": {}, "whom": {}, "whose": {}, "where": {},
	"go": {}, "no": {}, "not": {}, "yes": {}, "ok": {},
	"any": {}, "all": {}, "some": {}, "few": {}, "many": {}, "much": {}, "more": {}, "most": {},
	"new": {}, "old": {}, "now": {}, "here": {}, "there": {},
	"good": {}, "bad": {}, "big": {}, "small": {}, "high": {}, "low": {},
	"like": {}, "use": {}, "using": {}, "used": {}, "see": {}, "look": {}, "find": {},
	"think": {}, "know": {}, "feel": {}, "want": {}, "need": {},
	"thing": {}, "way": {}, "case": {}, "time": {}, "team": {},
	"actually": {}, "really": {}, "just": {}, "very": {}, "also": {}, "still": {},
	"about": {}, "across": {}, "through": {}, "between": {}, "during": {},
	"investigate": {}, "explain": {}, "show": {}, "tell": {},
}

var (
	// camelRE matches CamelCase identifiers (incl. acronyms like "JWT").
	// Requires at least one uppercase boundary so we don't match every
	// capitalized sentence-starter.
	camelRE = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]*)+\b`)

	// snakeRE matches snake_case identifiers — at least one underscore.
	snakeRE = regexp.MustCompile(`\b[a-z][a-z0-9]+(?:_[a-z0-9]+)+\b`)

	// callRE matches `name(` patterns — likely function calls in the
	// prompt body (e.g., "we call Greet() everywhere"). The bare name
	// without parens is captured.
	callRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]+)\s*\(`)

	// quotedRE matches double- or single-quoted spans. Captures the
	// inner content, drops the quotes.
	quotedRE = regexp.MustCompile(`['"]([^'"]+)['"]`)
)

// ExtractIdentifiers pulls code-shaped tokens (CamelCase, snake_case,
// fn-call patterns) out of a natural-language prompt. Each unique
// token is returned in source order. Common English words are dropped
// even if they happen to look like an identifier — keeps the find_def
// lookups focused on real symbols.
func ExtractIdentifiers(prompt string) []string {
	seen := map[string]struct{}{}
	var out []string

	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		// Drop trivial / common words — case-insensitive match against
		// the lowercase form so "The" / "the" / "THE" all filter out.
		if _, drop := commonWords[strings.ToLower(name)]; drop {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	for _, m := range camelRE.FindAllString(prompt, -1) {
		add(m)
	}
	for _, m := range snakeRE.FindAllString(prompt, -1) {
		add(m)
	}
	for _, m := range callRE.FindAllStringSubmatch(prompt, -1) {
		// m[1] is the captured bare name (without the trailing paren).
		add(m[1])
	}
	return out
}

// ExtractQuoted pulls quoted literals out of a prompt — useful for
// driving grep_symbols (literal=true) since users tend to quote the
// magic strings they're chasing (env-var names, ticker symbols,
// hardcoded constants, SQL fragments). Trivial 1-2 char strings are
// dropped; longer ones survive.
func ExtractQuoted(prompt string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range quotedRE.FindAllStringSubmatch(prompt, -1) {
		s := strings.TrimSpace(m[1])
		if len(s) < 3 {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
