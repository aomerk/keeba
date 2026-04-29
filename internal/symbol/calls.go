package symbol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// CallEdge is one (caller → callee) link extracted from a source file.
// The Caller fields identify the enclosing symbol (or "<file>" for
// top-level expressions); Callee is the bare name that appeared at
// CallerLine. Resolution to a Symbol happens at query time so we don't
// have to disambiguate ambiguous names at compile time.
type CallEdge struct {
	Caller     string `json:"caller"`
	CallerFile string `json:"caller_file"`
	CallerLine int    `json:"caller_line"`
	Callee     string `json:"callee"`
}

// callsExtractor is the per-language extractor for call edges. Each
// implementation operates on a single file's source.
type callsExtractor interface {
	ExtractCalls(file string, src []byte) []CallEdge
}

var callExtractors = map[string]callsExtractor{}

func init() {
	callExtractors["go"] = goCallsExtractor{}
	for lang := range regexExtractorsByLang {
		callExtractors[lang] = regexCallsExtractor{lang: lang}
	}
}

// ExtractCalls dispatches by language. Returns nil for unsupported
// extensions so callers can ignore unknown files cleanly.
func ExtractCalls(file string, src []byte) []CallEdge {
	lang := detectLanguage(file)
	ex, ok := callExtractors[lang]
	if !ok {
		return nil
	}
	return ex.ExtractCalls(file, src)
}

// goCallsExtractor walks the AST. Most accurate of the bunch:
// CallExpr.Fun resolves cleanly into either an Ident (function call) or
// a SelectorExpr (method / package call); we surface the rightmost
// identifier in either case so the inverse index keys on the bare name.
type goCallsExtractor struct{}

func (goCallsExtractor) ExtractCalls(file string, src []byte) []CallEdge {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return nil
	}

	var edges []CallEdge

	// Track the enclosing function/method for each call so the agent
	// can answer "who calls X" with concrete caller symbols, not just
	// "this file does".
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Body == nil {
			continue
		}
		caller := fn.Name.Name
		callerLine := fset.Position(fn.Pos()).Line

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := goCallName(call.Fun)
			if name == "" || name == caller {
				return true
			}
			edges = append(edges, CallEdge{
				Caller:     caller,
				CallerFile: file,
				CallerLine: fset.Position(call.Pos()).Line,
				Callee:     name,
			})
			return true
		})
		_ = callerLine
	}
	return edges
}

// goCallName picks the bare identifier from a Go CallExpr's Fun:
//
//	foo()      → "foo"
//	pkg.Foo()  → "Foo"
//	x.M()      → "M"
//	(*T).Bar() → "Bar"
//	make(...)  → "" (built-ins are noise; dropped)
func goCallName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		if isGoBuiltin(t.Name) {
			return ""
		}
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.IndexExpr: // generic Foo[T]() — call lives on .X
		return goCallName(t.X)
	case *ast.IndexListExpr:
		return goCallName(t.X)
	case *ast.ParenExpr:
		return goCallName(t.X)
	}
	return ""
}

func isGoBuiltin(name string) bool {
	switch name {
	case "make", "new", "len", "cap", "append", "copy", "delete",
		"close", "panic", "recover", "print", "println", "complex",
		"real", "imag", "min", "max", "clear":
		return true
	}
	return false
}

// regexCallsExtractor is the fallback for non-Go languages. It scans
// for `<ident>(` at any column, filters out keywords / definitions /
// builtins for the language. Caller is a best-effort lookup against
// the parallel-extracted Symbol set in the same file (caller is the
// last symbol whose start_line ≤ call line and end_line ≥ call line).
type regexCallsExtractor struct {
	lang string
}

var (
	callPatternBase = regexp.MustCompile(`(?m)\b(?P<name>[A-Za-z_]\w*)\s*\(`)

	// Keywords / identifiers that look like calls but aren't.
	regexCallNoise = map[string]map[string]struct{}{
		"py": {
			"if": {}, "while": {}, "for": {}, "return": {}, "yield": {},
			"def": {}, "class": {}, "lambda": {}, "print": {},
			"isinstance": {}, "type": {}, "len": {}, "range": {}, "str": {},
			"int": {}, "float": {}, "list": {}, "dict": {}, "set": {},
			"tuple": {}, "bool": {}, "None": {}, "True": {}, "False": {},
			"super": {},
		},
		"js": {
			"if": {}, "while": {}, "for": {}, "switch": {}, "catch": {},
			"return": {}, "function": {}, "class": {}, "new": {},
			"typeof": {}, "instanceof": {}, "console": {},
		},
		"ts": {
			"if": {}, "while": {}, "for": {}, "switch": {}, "catch": {},
			"return": {}, "function": {}, "class": {}, "new": {},
			"typeof": {}, "instanceof": {}, "interface": {}, "type": {},
			"enum": {}, "console": {},
		},
		"rs": {
			"if": {}, "while": {}, "for": {}, "match": {}, "loop": {},
			"return": {}, "fn": {}, "let": {}, "use": {}, "mod": {},
			"struct": {}, "enum": {}, "trait": {}, "impl": {},
			"println": {}, "print": {}, "vec": {}, "format": {},
		},
		"java": {
			"if": {}, "while": {}, "for": {}, "switch": {}, "catch": {},
			"return": {}, "new": {}, "instanceof": {}, "synchronized": {},
		},
		"kt": {
			"if": {}, "while": {}, "for": {}, "when": {}, "catch": {},
			"return": {}, "fun": {}, "val": {}, "var": {},
		},
		"rb": {
			"if": {}, "unless": {}, "while": {}, "until": {}, "for": {},
			"return": {}, "def": {}, "class": {}, "module": {}, "do": {},
		},
		"c": {
			"if": {}, "while": {}, "for": {}, "switch": {}, "return": {},
			"sizeof": {}, "typedef": {},
		},
		"cpp": {
			"if": {}, "while": {}, "for": {}, "switch": {}, "return": {},
			"sizeof": {}, "typedef": {}, "new": {}, "delete": {},
			"throw": {}, "catch": {},
		},
	}
)

func (r regexCallsExtractor) ExtractCalls(file string, src []byte) []CallEdge {
	noise := regexCallNoise[r.lang]
	matches := callPatternBase.FindAllSubmatchIndex(src, -1)
	if len(matches) == 0 {
		return nil
	}

	// Pre-compute the byte offset of every line start for O(log N)
	// line-of(byte) lookups.
	lineStarts := []int{0}
	for i, b := range src {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineOf := func(off int) int {
		// binary search
		lo, hi := 0, len(lineStarts)
		for lo < hi {
			mid := (lo + hi) / 2
			if lineStarts[mid] <= off {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return lo // 1-based
	}

	// First pass: extract the file's symbol surface so we can attribute
	// each call to its enclosing definition. If extraction fails we
	// still return the calls with caller="<file>" (better partial graph
	// than no graph).
	symRanges := regexExtractor{
		lang: r.lang,
		rx:   regexExtractorsByLang[r.lang],
	}
	syms, _ := symRanges.Extract(file, src)
	sort.Slice(syms, func(i, j int) bool { return syms[i].StartLine < syms[j].StartLine })

	enclosing := func(line int) string {
		// Walk backwards to find the most recent symbol that starts
		// before `line` and ends at-or-after it.
		for i := len(syms) - 1; i >= 0; i-- {
			s := syms[i]
			if s.StartLine <= line && line <= s.EndLine {
				return s.Name
			}
		}
		return "<file>"
	}

	var edges []CallEdge
	idx := callPatternBase.SubexpIndex("name")
	for _, m := range matches {
		nameLo, nameHi := m[idx*2], m[idx*2+1]
		callee := string(src[nameLo:nameHi])
		if _, drop := noise[callee]; drop {
			continue
		}
		// Skip the def site itself (the regex matches `def foo(`).
		// Best signal: the line's leading text is a definition keyword.
		startOfLine := lineStarts[lineOf(nameLo)-1]
		prefix := strings.TrimSpace(string(src[startOfLine:nameLo]))
		if isDefinitionPrefix(prefix) {
			continue
		}

		callerLine := lineOf(nameLo)
		caller := enclosing(callerLine)
		if caller == callee {
			continue // self-recursive call — keep would inflate noise
		}
		edges = append(edges, CallEdge{
			Caller:     caller,
			CallerFile: file,
			CallerLine: callerLine,
			Callee:     callee,
		})
	}
	return edges
}

// isDefinitionPrefix returns true if the trimmed prefix-of-line ends in
// a definition keyword — meaning the upcoming `name(` is a definition,
// not a call. Cheap to compute, language-agnostic enough.
func isDefinitionPrefix(prefix string) bool {
	for _, kw := range []string{
		"def", "function", "func", "fn", "class", "interface", "type",
		"async def", "async function", "pub fn", "pub async fn",
	} {
		if prefix == kw || strings.HasSuffix(prefix, " "+kw) {
			return true
		}
	}
	return false
}
