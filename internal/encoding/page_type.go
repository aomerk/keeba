package encoding

import (
	"path/filepath"
	"regexp"
	"strings"
)

// PageType classifies a wiki page so the bench grid can pick a
// page-type-aware encoding. Plan §10's central idea: structural-card
// wins on function pages, glossary+caveman wins on narrative, dense-tuple
// wins on entity-fact pages — applying the wrong one costs quality.
type PageType string

const (
	// PageTypeFunction — page documents one or more functions / classes.
	// Source-imported pages from code files land here.
	PageTypeFunction PageType = "function"
	// PageTypeEntity — short page of facts: bullet list of properties /
	// addresses / contracts / endpoints. Often a "what is X" page.
	PageTypeEntity PageType = "entity"
	// PageTypeNarrative — flowing prose. Investigations, decisions, README
	// imports. Default for anything not matching the others.
	PageTypeNarrative PageType = "narrative"
)

var codeFileExts = map[string]struct{}{
	".py": {}, ".pyx": {},
	".go": {},
	".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {},
	".ts": {}, ".tsx": {},
	".rs":   {},
	".java": {}, ".kt": {},
	".rb": {},
	".c":  {}, ".h": {}, ".cpp": {}, ".cc": {}, ".hpp": {},
	".swift": {}, ".scala": {},
}

// codeDefREs are the source-language signature shapes we recognize.
// Split into multiple alternations because Go's regexp lacks the lookahead
// expressivity to handle Python's `class Foo:` (no paren/brace after name)
// in the same expression as Go's `func foo(...)`.
var codeDefREs = []*regexp.Regexp{
	// def foo( | func foo( | function foo( | fn foo( | fn foo<
	regexp.MustCompile(`(?m)^\s*(?:async\s+)?(?:def|func|function|fn)\s+[A-Za-z_]\w*\s*[(<]`),
	// class Foo: | class Foo< | class Foo( | class Foo {  ; impl Foo {
	regexp.MustCompile(`(?m)^\s*(?:class|impl)\s+[A-Za-z_]\w*[\s({:<]`),
}

func hasCodeDefs(body string) bool {
	for _, re := range codeDefREs {
		if re.MatchString(body) {
			return true
		}
	}
	return false
}

// bulletFactRE matches an "entity-page" line: a short "- key: value" or
// "- key — value" entry with no nested prose beyond the value.
var bulletFactRE = regexp.MustCompile(`(?m)^\s*[-*]\s+\*?\*?[A-Za-z][A-Za-z0-9_/\- ]{0,40}\*?\*?\s*[:—]\s*\S`)

// DetectPageType heuristically classifies a wiki page from its body
// (frontmatter already stripped) plus any cited_files paths from the
// page's frontmatter. Cited code files are the strongest signal; body
// shape is the fallback.
func DetectPageType(body string, citedFiles []string) PageType {
	for _, f := range citedFiles {
		ext := strings.ToLower(filepath.Ext(f))
		if _, ok := codeFileExts[ext]; ok {
			return PageTypeFunction
		}
	}

	if hasCodeDefs(body) {
		return PageTypeFunction
	}

	if isEntityPage(body) {
		return PageTypeEntity
	}

	return PageTypeNarrative
}

// isEntityPage returns true when the body is dominated by short "- key:
// value" bullet lines with little prose. Threshold: at least 4 fact-shaped
// bullets AND fact-bullets account for >= 50% of non-blank lines.
func isEntityPage(body string) bool {
	matches := bulletFactRE.FindAllString(body, -1)
	if len(matches) < 4 {
		return false
	}
	nonBlank := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			nonBlank++
		}
	}
	if nonBlank == 0 {
		return false
	}
	return float64(len(matches))/float64(nonBlank) >= 0.5
}
