package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/symbol"
)

// Options tunes the Build behavior. Zero value = sensible defaults.
type Options struct {
	// MaxBytes caps the rendered markdown size — useful when piping into
	// a tool with a context window. Zero = no cap.
	MaxBytes int
	// MaxNameHits caps find_def-equivalent results per identifier.
	// Default 5.
	MaxNameHits int
	// MaxBM25Hits caps the BM25 result count. Default 10.
	MaxBM25Hits int
	// MaxLiteralHits caps grep-equivalent results per quoted literal.
	// Default 10.
	MaxLiteralHits int
}

// NameHit is one find_def-shaped hit from an identifier in the prompt.
type NameHit struct {
	Identifier string        `json:"identifier"`
	Symbol     symbol.Symbol `json:"symbol"`
}

// LiteralHit is one body-grep result for a quoted literal.
type LiteralHit struct {
	Literal string        `json:"literal"`
	Symbol  symbol.Symbol `json:"symbol"`
	Line    int           `json:"line"`
	Snippet string        `json:"snippet"`
}

// Report is the structured result of a context Build. RenderMarkdown
// turns it into the paste-ready text; --json mode dumps the struct.
type Report struct {
	RepoPath    string             `json:"repo_path"`
	Prompt      string             `json:"prompt"`
	Idents      []string           `json:"idents"`
	Quoted      []string           `json:"quoted"`
	NameHits    []NameHit          `json:"name_hits"`
	BM25Hits    []symbol.SearchHit `json:"bm25_hits"`
	LiteralHits []LiteralHit       `json:"literal_hits"`
	// MaxBytes is the post-render cap applied by RenderMarkdown. Carried
	// on the report so JSON callers see the same setting that drove the
	// markdown view.
	MaxBytes int `json:"max_bytes,omitempty"`
}

// Build runs the symbol-graph queries an agent would have run for this
// prompt and returns a structured Report. The CLI's day-1 demo: works
// without MCP integration. Caller can render markdown for paste, or
// dump JSON for scripted use.
//
// Loads .keeba/symbols.json from repoPath; missing graph is a hard
// error (the user needs to `keeba compile` first).
func Build(repoPath, prompt string, opts Options) (Report, error) {
	if opts.MaxNameHits <= 0 {
		opts.MaxNameHits = 5
	}
	if opts.MaxBM25Hits <= 0 {
		opts.MaxBM25Hits = 10
	}
	if opts.MaxLiteralHits <= 0 {
		opts.MaxLiteralHits = 10
	}

	idx, err := symbol.Load(repoPath)
	if err != nil {
		return Report{}, fmt.Errorf("load symbol graph (run `keeba compile` first?): %w", err)
	}

	rep := Report{
		RepoPath: repoPath,
		Prompt:   prompt,
		Idents:   ExtractIdentifiers(prompt),
		Quoted:   ExtractQuoted(prompt),
		MaxBytes: opts.MaxBytes,
	}

	// 1) Identifier lookups — equivalent to find_def per ident.
	byName := buildByName(idx.Symbols)
	for _, name := range rep.Idents {
		matches := byName[name]
		if len(matches) > opts.MaxNameHits {
			matches = matches[:opts.MaxNameHits]
		}
		for _, sym := range matches {
			rep.NameHits = append(rep.NameHits, NameHit{Identifier: name, Symbol: sym})
		}
	}

	// 2) BM25 over name+sig+doc — equivalent to search_symbols on the
	// whole prompt as a free-text query.
	bm := symbol.BuildBM25Index(idx.Symbols)
	rep.BM25Hits = bm.Query(prompt, opts.MaxBM25Hits)

	// 3) Literal grep — equivalent to grep_symbols literal=true per
	// quoted string. Keeps it tight: one hit per symbol body to avoid
	// runaway noise.
	for _, lit := range rep.Quoted {
		hits := scanLiteral(repoPath, idx.Symbols, lit, opts.MaxLiteralHits)
		rep.LiteralHits = append(rep.LiteralHits, hits...)
	}

	return rep, nil
}

// buildByName mirrors the unexported helper in internal/symbol/live.go.
// Duplicated here to keep the context package free of LiveIndex's
// fsnotify machinery — we don't need watching, just a static lookup.
func buildByName(syms []symbol.Symbol) map[string][]symbol.Symbol {
	out := make(map[string][]symbol.Symbol, len(syms))
	for _, s := range syms {
		out[s.Name] = append(out[s.Name], s)
	}
	return out
}

// scanLiteral reads each touched file once and returns up to limit
// hits where literal appears inside a symbol's body. One hit per
// symbol max — tight summary, not exhaustive listing.
func scanLiteral(repoRoot string, syms []symbol.Symbol, literal string, limit int) []LiteralHit {
	if literal == "" || limit <= 0 {
		return nil
	}
	re, err := regexp.Compile(regexp.QuoteMeta(literal))
	if err != nil {
		return nil
	}

	bySrc := map[string][]symbol.Symbol{}
	for _, s := range syms {
		bySrc[s.File] = append(bySrc[s.File], s)
	}

	var out []LiteralHit
	files := make([]string, 0, len(bySrc))
	for f := range bySrc {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, file := range files {
		if len(out) >= limit {
			break
		}
		body, err := os.ReadFile(filepath.Join(repoRoot, file)) //nolint:gosec
		if err != nil {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for _, sym := range bySrc[file] {
			if len(out) >= limit {
				break
			}
			startIdx := sym.StartLine - 1
			end := min(sym.EndLine, len(lines))
			for i := startIdx; i < end; i++ {
				if re.MatchString(lines[i]) {
					out = append(out, LiteralHit{
						Literal: literal,
						Symbol:  sym,
						Line:    i + 1,
						Snippet: snippetTrim(lines[i]),
					})
					break // one hit per symbol body — keeps the report tight
				}
			}
		}
	}
	return out
}

// snippetTrim mirrors the MCP grep_symbols snippet helper — trim
// leading whitespace and cap at snippetMaxLen runes with an ellipsis.
const snippetMaxLen = 200

func snippetTrim(line string) string {
	s := strings.TrimLeft(line, " \t")
	if len(s) > snippetMaxLen {
		s = s[:snippetMaxLen-1] + "…"
	}
	return s
}
