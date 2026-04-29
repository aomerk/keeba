package encoding

import (
	"regexp"
	"strings"
)

// MDCaveman is the deterministic md-caveman encoder. Drops articles,
// auxiliaries, and common filler tokens; rewrites a small set of
// multi-word phrases. Stateless. ~25-40% reduction on technical prose
// with no model dependency.
type MDCaveman struct{}

// Name returns "md-caveman".
func (MDCaveman) Name() string { return "md-caveman" }

var (
	cavemanWordRE  = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9_'-]*\b`)
	cavemanWSRE    = regexp.MustCompile(`\s+`)
	cavemanPunctRE = regexp.MustCompile(`\s+([,.;:!?])`)
)

var cavemanDrop = map[string]struct{}{
	// articles
	"a": {}, "an": {}, "the": {},
	// copula / aux
	"is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "am": {},
	"do": {}, "does": {}, "did": {}, "doing": {}, "done": {},
	"has": {}, "have": {}, "had": {}, "having": {},
	// hedges / filler
	"just": {}, "really": {}, "basically": {}, "actually": {},
	"simply": {}, "very": {}, "quite": {}, "rather": {},
	"somewhat": {}, "perhaps": {}, "maybe": {}, "probably": {},
	"essentially": {}, "literally": {}, "honestly": {},
	"obviously": {}, "clearly": {}, "kind": {}, "sort": {},
	// politeness
	"please": {}, "thanks": {}, "thank": {}, "kindly": {},
	// weak adverbs
	"also": {}, "however": {}, "though": {}, "although": {},
	"thus": {}, "hence": {}, "therefore": {}, "moreover": {},
	"furthermore": {}, "additionally": {}, "besides": {}, "indeed": {},
}

type cavemanRewrite struct {
	pat *regexp.Regexp
	rep string
}

var cavemanRewrites = []cavemanRewrite{
	{regexp.MustCompile(`(?i)\bin order to\b`), "to"},
	{regexp.MustCompile(`(?i)\bdue to the fact that\b`), "because"},
	{regexp.MustCompile(`(?i)\bin spite of\b`), "despite"},
	{regexp.MustCompile(`(?i)\bas well as\b`), "and"},
	{regexp.MustCompile(`(?i)\bsuch that\b`), "so"},
	{regexp.MustCompile(`(?i)\bso that\b`), "so"},
	{regexp.MustCompile(`(?i)\bdoes not\b`), "not"},
	{regexp.MustCompile(`(?i)\bdo not\b`), "not"},
	{regexp.MustCompile(`(?i)\bis not\b`), "not"},
	{regexp.MustCompile(`(?i)\bare not\b`), "not"},
	{regexp.MustCompile(`(?i)\bcannot\b`), "cant"},
	{regexp.MustCompile(`(?i)\bcan not\b`), "cant"},
	{regexp.MustCompile(`(?i)\bwill not\b`), "wont"},
}

// Encode applies caveman compression: multi-word rewrites, then
// token-level drops, then whitespace + punctuation cleanup.
func (MDCaveman) Encode(body string) (string, error) {
	for _, r := range cavemanRewrites {
		body = r.pat.ReplaceAllString(body, r.rep)
	}

	body = cavemanWordRE.ReplaceAllStringFunc(body, func(w string) string {
		if _, drop := cavemanDrop[strings.ToLower(w)]; drop {
			return ""
		}
		return w
	})

	body = cavemanWSRE.ReplaceAllString(body, " ")
	body = cavemanPunctRE.ReplaceAllString(body, "$1")
	return strings.TrimSpace(body), nil
}
