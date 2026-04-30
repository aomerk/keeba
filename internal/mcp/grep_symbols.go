package mcp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/symbol"
)

// grepSymbolsArgs is the argument shape for the grep_symbols tool.
type grepSymbolsArgs struct {
	Pattern      string `json:"pattern"`                  // RE2 regex (or literal if Literal=true)
	Literal      bool   `json:"literal,omitempty"`        // wrap pattern with regexp.QuoteMeta
	File         string `json:"file,omitempty"`           // path-prefix filter
	Language     string `json:"language,omitempty"`       // go, py, ts, ...
	Kind         string `json:"kind,omitempty"`           // function, method, class, ...
	Limit        int    `json:"limit,omitempty"`          // total hits (default 25, max 200)
	MaxPerSymbol int    `json:"max_per_symbol,omitempty"` // hits per symbol body (default 5, max 20)
}

// grepHit is one match inside a symbol body.
type grepHit struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`    // absolute, 1-based
	Col     int    `json:"col"`     // 1-based byte column of match start
	Snippet string `json:"snippet"` // matching line, trimmed/ellipsized
}

const (
	grepPatternMaxLen    = 1024
	grepDefaultLimit     = 25
	grepMaxLimit         = 200
	grepDefaultPerSymbol = 5
	grepMaxPerSymbol     = 20
	grepSnippetMaxLen    = 200
)

// toolGrepSymbols runs an RE2 regex over each symbol body and returns
// ranked match snippets. Closes the last gap in the symbol-graph MCP
// surface: search_symbols ranks by name/sig/doc; grep_symbols handles
// terms that live inside the body itself (env vars, magic strings, SQL
// fragments, hardcoded URLs). Pairs with read_chunk: grep_symbols
// locates, read_chunk pulls surrounding context if needed.
func (s *Server) toolGrepSymbols(raw json.RawMessage) rpcResponse {
	if s.live == nil {
		return notCompiledResponse()
	}
	var a grepSymbolsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "pattern is required"}}
	}
	if len(a.Pattern) > grepPatternMaxLen {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "pattern too long (max 1024)"}}
	}
	pattern := a.Pattern
	if a.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "invalid regex: " + err.Error()}}
	}

	limit := a.Limit
	if limit <= 0 {
		limit = grepDefaultLimit
	}
	if limit > grepMaxLimit {
		limit = grepMaxLimit
	}
	maxPerSym := a.MaxPerSymbol
	if maxPerSym <= 0 {
		maxPerSym = grepDefaultPerSymbol
	}
	if maxPerSym > grepMaxPerSymbol {
		maxPerSym = grepMaxPerSymbol
	}

	// Validate the file-prefix filter against the repo root before any
	// walk — fast rejection of /etc/passwd-style escapes. The filter is
	// a prefix (not a single file), so we only need safeJoin on the
	// directory it points to.
	root := s.cfg.WikiRoot
	filePrefix := strings.TrimSpace(a.File)
	if filePrefix != "" {
		if _, err := safeJoin(root, filePrefix); err != nil {
			return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
		}
	}

	syms := filterSymbols(s.live.Symbols(), filePrefix, a.Language, a.Kind)
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].File != syms[j].File {
			return syms[i].File < syms[j].File
		}
		return syms[i].StartLine < syms[j].StartLine
	})

	hits := make([]grepHit, 0, limit)
	truncated := false

	var (
		curFile  string
		curLines []string
		curOK    bool // false if the current file failed to load (skip cluster)
	)

	// truncated is set only when the limit guard at the top fires — that
	// proves there was at least one unwalked symbol when we capped. If the
	// last symbol's matches happen to land exactly on `limit`, the guard
	// never fires and truncated stays false: we walked the whole graph.
	for _, sym := range syms {
		if len(hits) >= limit {
			truncated = true
			break
		}
		if sym.File != curFile {
			curFile = sym.File
			curLines, curOK = loadFileLines(root, sym.File)
		}
		if !curOK {
			continue
		}
		hits = appendBodyHits(hits, sym, curLines, re, maxPerSym, limit)
	}

	body, err := json.MarshalIndent(map[string]any{
		"pattern":   a.Pattern,
		"count":     len(hits),
		"truncated": truncated,
		"hits":      hits,
	}, "", "  ")
	if err != nil {
		return rpcResponse{Error: &rpcError{Code: -32603, Message: "encode: " + err.Error()}}
	}
	return rpcResponse{Result: map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(body),
		}},
	}}
}

// filterSymbols applies the prefix / language / kind filters and
// returns a freshly-allocated slice. Always allocates — never aliases
// the input — so callers can pass any slice without worrying about
// surprise mutation. The micro-allocation cost is dwarfed by the
// regex sweep that follows.
func filterSymbols(in []symbol.Symbol, filePrefix, language, kind string) []symbol.Symbol {
	out := make([]symbol.Symbol, 0, len(in))
	for _, sym := range in {
		if filePrefix != "" && !strings.HasPrefix(sym.File, filePrefix) {
			continue
		}
		if language != "" && sym.Language != language {
			continue
		}
		if kind != "" && sym.Kind != kind {
			continue
		}
		out = append(out, sym)
	}
	return out
}

// loadFileLines reads a repo-relative file and splits it into lines.
// Missing file is treated as "skip" (graph lags disk after a rename or
// delete) — returns nil, false. Path safety via safeJoin.
func loadFileLines(root, rel string) ([]string, bool) {
	abs, err := safeJoin(root, rel)
	if err != nil {
		return nil, false
	}
	body, err := os.ReadFile(abs) //nolint:gosec // bounded by safeJoin
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false
		}
		return nil, false
	}
	return strings.Split(string(body), "\n"), true
}

// appendBodyHits scans sym's body lines for re matches. Caps at
// perSymCap matches per body and stops once the global limit is reached.
// Snippet is the matching line, trimmed and ellipsized at grepSnippetMaxLen.
func appendBodyHits(hits []grepHit, sym symbol.Symbol, lines []string, re *regexp.Regexp, perSymCap, globalLimit int) []grepHit {
	if sym.StartLine <= 0 {
		return hits
	}
	startIdx := sym.StartLine - 1
	end := min(sym.EndLine, len(lines))
	if end < sym.StartLine {
		return hits
	}
	perSym := 0
	for i := startIdx; i < end; i++ {
		if perSym >= perSymCap || len(hits) >= globalLimit {
			break
		}
		line := lines[i]
		matches := re.FindAllStringIndex(line, perSymCap-perSym)
		for _, m := range matches {
			if perSym >= perSymCap || len(hits) >= globalLimit {
				break
			}
			hits = append(hits, grepHit{
				Name:    sym.Name,
				Kind:    sym.Kind,
				File:    sym.File,
				Line:    i + 1,
				Col:     m[0] + 1,
				Snippet: snippetTrim(line),
			})
			perSym++
		}
	}
	return hits
}

// snippetTrim returns line trimmed of leading whitespace and capped at
// grepSnippetMaxLen runes. Long lines get an ellipsis suffix so the
// agent sees something rather than truncated tokens.
func snippetTrim(line string) string {
	s := strings.TrimLeft(line, " \t")
	if len(s) > grepSnippetMaxLen {
		s = s[:grepSnippetMaxLen-1] + "…"
	}
	return s
}

// grepSymbolsAlternative computes "bytes the agent would have read_file'd"
// for the grep_symbols call: sum of distinct file sizes the same filters
// would have touched. Re-walks the symbol slice (cheap — already a copy)
// and stats each unique file. Powers the SessionStats receipt.
func (s *Server) grepSymbolsAlternative(root string, raw json.RawMessage) int {
	if s.live == nil {
		return 0
	}
	var a grepSymbolsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return 0
	}
	syms := filterSymbols(s.live.Symbols(), strings.TrimSpace(a.File), a.Language, a.Kind)
	seen := map[string]struct{}{}
	total := 0
	for _, sym := range syms {
		if _, dup := seen[sym.File]; dup {
			continue
		}
		seen[sym.File] = struct{}{}
		abs, err := safeJoin(root, sym.File)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		total += int(info.Size())
	}
	return total
}
