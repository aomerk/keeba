package symbol

import (
	"regexp"
	"strings"
)

// regexPattern describes one symbol shape: a compiled regex with named
// groups (`name`, `recv`) plus the kind label and an optional language
// override (the `extends` shapes write more specific kinds than the
// generic Extractor.lang).
type regexPattern struct {
	re   *regexp.Regexp
	kind string
}

// regexExtractor runs a slice of patterns over the source. Patterns are
// language-specific; the extractor's `lang` field stamps Symbol.Language.
type regexExtractor struct {
	lang string
	rx   []regexPattern
}

func (r regexExtractor) Extract(file string, src []byte) ([]Symbol, error) {
	text := string(src)
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return nil, nil
	}

	// Pre-compute line-start byte offsets so we can map regex match
	// positions back to (line, col) in O(1).
	starts := make([]int, len(lines)+1)
	pos := 0
	for i, ln := range lines {
		starts[i] = pos
		pos += len(ln) + 1 // +1 for the '\n' lost by Split
	}
	starts[len(lines)] = len(text)

	lineOf := func(byteOff int) int {
		// Linear is fine — patterns return small N matches per file.
		for i := 1; i < len(starts); i++ {
			if starts[i] > byteOff {
				return i
			}
		}
		return len(lines)
	}

	var out []Symbol
	for _, pat := range r.rx {
		for _, m := range pat.re.FindAllSubmatchIndex(src, -1) {
			groupName := submatchStr(src, m, pat.re, "name")
			if groupName == "" {
				continue
			}
			receiver := submatchStr(src, m, pat.re, "recv")
			startLine := lineOf(m[0])
			sig := strings.TrimSpace(lines[startLine-1])
			if len(sig) > 200 {
				sig = sig[:200]
			}
			out = append(out, Symbol{
				Name:      groupName,
				Kind:      pat.kind,
				File:      file,
				StartLine: startLine,
				// EndLine for regex extractors is best-effort; we use the
				// next blank-line-or-EOF as the end. Cheap and good
				// enough for "where is this defined" UX.
				EndLine:   estimateEndLine(lines, startLine),
				Signature: sig,
				Receiver:  receiver,
				Language:  r.lang,
			})
		}
	}
	return out, nil
}

func submatchStr(src []byte, m []int, re *regexp.Regexp, group string) string {
	idx := re.SubexpIndex(group)
	if idx < 0 || idx*2+1 >= len(m) || m[idx*2] < 0 {
		return ""
	}
	return string(src[m[idx*2]:m[idx*2+1]])
}

// estimateEndLine walks forward from start until it sees two consecutive
// blank lines, an EOF, or a less-indented sibling line. Returns the
// 1-based line number of the symbol's *last meaningful line* (so
// `start_line..end_line` is the inclusive range of the symbol's body).
// Heuristic — fine for "find_def" UX where the user just wants a small
// context chunk to render.
func estimateEndLine(lines []string, start int) int {
	if start <= 0 || start > len(lines) {
		return start
	}
	startIndent := leadingSpace(lines[start-1])
	lastMeaningful := start // start line is always part of the symbol
	blank := 0
	for i := start; i < len(lines); i++ {
		ln := lines[i]
		trim := strings.TrimSpace(ln)
		if trim == "" {
			blank++
			if blank >= 2 {
				break
			}
			continue
		}
		blank = 0
		if i > start && leadingSpace(ln) <= startIndent {
			isCont := strings.HasPrefix(trim, "}") || strings.HasPrefix(trim, "end") ||
				strings.HasPrefix(trim, ")") || strings.HasPrefix(trim, "]")
			if !isCont {
				break
			}
		}
		lastMeaningful = i + 1 // i is 0-based; lines are 1-based externally
	}
	return lastMeaningful
}

func leadingSpace(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// regexExtractorsByLang is the master table. Each entry is a language tag
// mapped to its ordered pattern list. Patterns are tried in order; first
// match wins per source position. Anchored to start-of-line (`(?m)^`)
// where possible to avoid false positives inside strings.
var regexExtractorsByLang = map[string][]regexPattern{
	"py": {
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:async\s+)?def\s+(?P<name>[A-Za-z_]\w*)\s*\(`),
			kind: "function",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*class\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "class",
		},
	},
	"js": {
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:async\s+)?function\s+(?P<name>[A-Za-z_]\w*)\s*\(`),
			kind: "function",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?class\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "class",
		},
		// const Foo = (... ) => { ...   and   const Foo = function (...) { ...
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:const|let|var)\s+(?P<name>[A-Za-z_]\w*)\s*=\s*(?:async\s+)?(?:\([^)]*\)\s*=>|function\s*\()`),
			kind: "function",
		},
	},
	"ts": {
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:async\s+)?function\s+(?P<name>[A-Za-z_]\w*)\s*[<(]`),
			kind: "function",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:abstract\s+)?class\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "class",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?interface\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "interface",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?type\s+(?P<name>[A-Za-z_]\w*)\s*=`),
			kind: "type",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:const|let|var)\s+(?P<name>[A-Za-z_]\w*)\s*=\s*(?:async\s+)?(?:\([^)]*\)\s*=>|function\s*\()`),
			kind: "function",
		},
	},
	"rs": {
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:pub\s+)?(?:async\s+)?fn\s+(?P<name>[A-Za-z_]\w*)\s*[<(]`),
			kind: "function",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:pub\s+)?struct\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "type",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:pub\s+)?enum\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "type",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:pub\s+)?trait\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "interface",
		},
		// impl Foo / impl<T> Foo<T> for Bar
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*impl(?:<[^>]*>)?\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "type",
		},
	},
	"java": {
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:public|private|protected)?\s*(?:static\s+)?(?:final\s+)?(?:abstract\s+)?class\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "class",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:public|private|protected)?\s*(?:static\s+)?interface\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "interface",
		},
		// public ReturnType methodName(args) { — keep type-name greedy short
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:public|private|protected)\s+(?:static\s+)?(?:final\s+)?[A-Za-z_][\w<>,\[\] ]*?\s+(?P<name>[A-Za-z_]\w*)\s*\([^)]*\)\s*[{;]`),
			kind: "method",
		},
	},
	"kt": {
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:public|private|internal|protected)?\s*(?:open|abstract|final)?\s*(?:suspend\s+)?fun\s+(?P<name>[A-Za-z_]\w*)\s*[<(]`),
			kind: "function",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:public|private|internal|protected)?\s*(?:open|abstract|sealed|data)?\s*class\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "class",
		},
	},
	"rb": {
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*def\s+(?:self\.)?(?P<name>[A-Za-z_][\w?!=]*)\b`),
			kind: "function",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*class\s+(?P<name>[A-Z][\w]*)\b`),
			kind: "class",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*module\s+(?P<name>[A-Z][\w]*)\b`),
			kind: "type",
		},
	},
	"c": {
		// C function-definition signature spans usually fit on one line:
		//   static int foo(int x, int y) {
		{
			re:   regexp.MustCompile(`(?m)^(?:static\s+|extern\s+)?(?:[A-Za-z_]\w*[ \t*]+)+(?P<name>[A-Za-z_]\w*)\s*\([^)]*\)\s*\{`),
			kind: "function",
		},
	},
	"cpp": {
		{
			re:   regexp.MustCompile(`(?m)^(?:static\s+|extern\s+)?(?:[A-Za-z_:][\w:]*[ \t*&]+)+(?P<name>[A-Za-z_]\w*)\s*\([^)]*\)\s*[{:]`),
			kind: "function",
		},
		{
			re:   regexp.MustCompile(`(?m)^[ \t]*(?:class|struct)\s+(?P<name>[A-Za-z_]\w*)\b`),
			kind: "class",
		},
	},
}
