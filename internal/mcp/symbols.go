package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/symbol"
)

// loadLiveSymbols tries to read the per-repo symbol index and wrap it
// in a fsnotify-watched LiveIndex. Missing index is not fatal — the
// symbol-graph tools (find_def, summary) just hint that the user
// should run `keeba compile`.
func loadLiveSymbols(repoRoot string) (*symbol.LiveIndex, error) {
	li, err := symbol.NewLiveIndex(repoRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return li, nil
}

// findDefArgs is the argument shape for the find_def tool.
type findDefArgs struct {
	Name     string `json:"name"`
	Language string `json:"language,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (s *Server) toolFindDef(raw json.RawMessage) rpcResponse {
	if s.live == nil {
		return notCompiledResponse()
	}
	var a findDefArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if strings.TrimSpace(a.Name) == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "name is required"}}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// Exact-match first; if nothing, try case-insensitive contains.
	matches := s.live.ByName(a.Name)
	if len(matches) == 0 {
		needle := strings.ToLower(a.Name)
		s.live.Names(func(name string, syms []symbol.Symbol) {
			if strings.Contains(strings.ToLower(name), needle) {
				matches = append(matches, syms...)
			}
		})
	}

	// Filter by language / kind if provided.
	if a.Language != "" || a.Kind != "" {
		filtered := matches[:0]
		for _, sym := range matches {
			if a.Language != "" && sym.Language != a.Language {
				continue
			}
			if a.Kind != "" && sym.Kind != a.Kind {
				continue
			}
			filtered = append(filtered, sym)
		}
		matches = filtered
	}

	// Stable order: by file then line, so MCP responses are diff-friendly.
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File != matches[j].File {
			return matches[i].File < matches[j].File
		}
		return matches[i].StartLine < matches[j].StartLine
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return s.symbolListByCodec(matches)
}

// findDefAlternative returns the file-size sum the agent would have
// pulled with `grep -rn "Name" .` + read each match. We replay the
// tool's own match logic (exact name, then case-insensitive contains,
// then language/kind filter) and stat the distinct files left standing.
// Bounded by the same limit the tool applies, so the receipt matches
// what the agent actually saw.
func (s *Server) findDefAlternative(root string, raw json.RawMessage) int {
	if s.live == nil {
		return 0
	}
	var a findDefArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return 0
	}
	if strings.TrimSpace(a.Name) == "" {
		return 0
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	matches := s.live.ByName(a.Name)
	if len(matches) == 0 {
		needle := strings.ToLower(a.Name)
		s.live.Names(func(name string, syms []symbol.Symbol) {
			if strings.Contains(strings.ToLower(name), needle) {
				matches = append(matches, syms...)
			}
		})
	}
	if a.Language != "" || a.Kind != "" {
		filtered := matches[:0]
		for _, sym := range matches {
			if a.Language != "" && sym.Language != a.Language {
				continue
			}
			if a.Kind != "" && sym.Kind != a.Kind {
				continue
			}
			filtered = append(filtered, sym)
		}
		matches = filtered
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	files := make([]string, 0, len(matches))
	for _, m := range matches {
		files = append(files, m.File)
	}
	return sumFileSizes(root, files)
}

// findCallersArgs is the argument shape for the find_callers tool.
type findCallersArgs struct {
	Name  string `json:"name"`
	File  string `json:"file,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// toolFindCallers returns every call edge whose callee matches name.
// Pairs with find_def: find_def says "X is here", find_callers says
// "and here are the N places X is called from". The agent now answers
// impact questions ("what would break if I rename X?") in two MCP
// calls, no grep loop.
func (s *Server) toolFindCallers(raw json.RawMessage) rpcResponse {
	if s.live == nil {
		return notCompiledResponse()
	}
	var a findCallersArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if strings.TrimSpace(a.Name) == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "name is required"}}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 200 {
		limit = 200
	}

	edges := s.live.CallersOf(a.Name)

	// Optional file/dir filter so the agent can ask "who calls X under
	// internal/auth/" without pulling the global call graph.
	if filePrefix := strings.TrimSpace(a.File); filePrefix != "" {
		filtered := edges[:0]
		for _, e := range edges {
			if strings.HasPrefix(e.CallerFile, filePrefix) {
				filtered = append(filtered, e)
			}
		}
		edges = filtered
	}

	// Stable order: file, then line, so consecutive runs diff cleanly.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].CallerFile != edges[j].CallerFile {
			return edges[i].CallerFile < edges[j].CallerFile
		}
		return edges[i].CallerLine < edges[j].CallerLine
	})
	if len(edges) > limit {
		edges = edges[:limit]
	}

	body, err := json.MarshalIndent(map[string]any{
		"callee": a.Name,
		"count":  len(edges),
		"edges":  edges,
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

// searchSymbolsArgs is the argument shape for the search_symbols tool.
type searchSymbolsArgs struct {
	Query    string `json:"query"`
	Limit    int    `json:"limit,omitempty"`
	Language string `json:"language,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

// toolSearchSymbols runs a BM25 query over the symbol index. Pairs with
// find_def: find_def needs the exact name; search_symbols handles the
// "what handles auth?" / "where's the JWT validation?" case where the
// agent has a concept but not a name. Score is included so callers can
// see why a hit ranked.
func (s *Server) toolSearchSymbols(raw json.RawMessage) rpcResponse {
	if s.live == nil {
		return notCompiledResponse()
	}
	var a searchSymbolsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if strings.TrimSpace(a.Query) == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "query is required"}}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// Pull a wider candidate set when filters are present so the post-filter
	// result still has limit hits.
	want := limit
	if a.Language != "" || a.Kind != "" {
		want = limit * 4
	}
	hits := s.live.SearchSymbols(a.Query, want)

	if a.Language != "" || a.Kind != "" {
		filtered := hits[:0]
		for _, h := range hits {
			if a.Language != "" && h.Symbol.Language != a.Language {
				continue
			}
			if a.Kind != "" && h.Symbol.Kind != a.Kind {
				continue
			}
			filtered = append(filtered, h)
		}
		hits = filtered
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}

	body, err := json.MarshalIndent(map[string]any{
		"query": a.Query,
		"count": len(hits),
		"hits":  hits,
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

// findCallersAlternative returns the file-size sum the agent would
// have pulled with `grep -rn "Name(" .` + read each match. The caller
// files of the result set are exactly that match list. Bounded by the
// tool's own limit + file-prefix filter so the receipt matches reality.
func (s *Server) findCallersAlternative(root string, raw json.RawMessage) int {
	if s.live == nil {
		return 0
	}
	var a findCallersArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return 0
	}
	if strings.TrimSpace(a.Name) == "" {
		return 0
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 200 {
		limit = 200
	}
	edges := s.live.CallersOf(a.Name)
	if filePrefix := strings.TrimSpace(a.File); filePrefix != "" {
		filtered := edges[:0]
		for _, e := range edges {
			if strings.HasPrefix(e.CallerFile, filePrefix) {
				filtered = append(filtered, e)
			}
		}
		edges = filtered
	}
	if len(edges) > limit {
		edges = edges[:limit]
	}
	files := make([]string, 0, len(edges))
	for _, e := range edges {
		files = append(files, e.CallerFile)
	}
	return sumFileSizes(root, files)
}

// summaryArgs is the argument shape for the summary tool.
type summaryArgs struct {
	File  string `json:"file,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

func (s *Server) toolSummary(raw json.RawMessage) rpcResponse {
	if s.live == nil {
		return notCompiledResponse()
	}
	var a summaryArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	wantPrefix := strings.TrimSpace(a.File)
	all := s.live.Symbols()
	out := make([]symbol.Symbol, 0, limit)
	for _, sym := range all {
		if wantPrefix != "" && !strings.HasPrefix(sym.File, wantPrefix) {
			continue
		}
		out = append(out, sym)
		if len(out) >= limit {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].StartLine < out[j].StartLine
	})
	return symbolListResponse(out)
}

// summaryAlternative returns the file-size sum for the distinct files
// the summary tool's walk would have surfaced. Without this tool the
// agent would `ls` the prefix and read each file — that's the alt.
func (s *Server) summaryAlternative(root string, raw json.RawMessage) int {
	if s.live == nil {
		return 0
	}
	var a summaryArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return 0
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	wantPrefix := strings.TrimSpace(a.File)
	all := s.live.Symbols()
	files := make([]string, 0, limit)
	for _, sym := range all {
		if wantPrefix != "" && !strings.HasPrefix(sym.File, wantPrefix) {
			continue
		}
		files = append(files, sym.File)
		if len(files) >= limit {
			break
		}
	}
	return sumFileSizes(root, files)
}

// searchSymbolsAlternative returns the file-size sum for the top-K BM25
// hits — the files an agent would have peeked at after a non-keeba grep
// for the query terms. Honest lower bound on what the BM25 ranking
// short-circuited. Replays the same query path used by the tool.
func (s *Server) searchSymbolsAlternative(root string, raw json.RawMessage) int {
	if s.live == nil {
		return 0
	}
	var a searchSymbolsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return 0
	}
	if strings.TrimSpace(a.Query) == "" {
		return 0
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	want := limit
	if a.Language != "" || a.Kind != "" {
		want = limit * 4
	}
	hits := s.live.SearchSymbols(a.Query, want)
	if a.Language != "" || a.Kind != "" {
		filtered := hits[:0]
		for _, h := range hits {
			if a.Language != "" && h.Symbol.Language != a.Language {
				continue
			}
			if a.Kind != "" && h.Symbol.Kind != a.Kind {
				continue
			}
			filtered = append(filtered, h)
		}
		hits = filtered
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	files := make([]string, 0, len(hits))
	for _, h := range hits {
		files = append(files, h.Symbol.File)
	}
	return sumFileSizes(root, files)
}

// symbolListResponse renders a slice of Symbols as the MCP content
// array Claude Code / Cursor / Codex consume. JSON pretty-print so the
// agent can read each field on its own line.
func symbolListResponse(syms []symbol.Symbol) rpcResponse {
	body, err := json.MarshalIndent(map[string]any{
		"count":   len(syms),
		"symbols": syms,
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

// leanRow is the lean-codec representation of a symbol — code + just
// enough metadata for the agent to reason ("AuthMiddleware lives at
// auth/mw.go:5-12, it's a function") without the full sig+doc payload.
// Sig + doc come back via the expand tool when the agent actually
// needs them.
type leanRow struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// symbolListByCodec dispatches to symbolListResponse (full codec) or
// the lean-row response based on s.Codec. In lean mode every symbol
// gets registered in the session codetable so the codes returned to
// the agent resolve correctly via the `expand` tool.
func (s *Server) symbolListByCodec(syms []symbol.Symbol) rpcResponse {
	if s.Codec != "lean" {
		return symbolListResponse(syms)
	}
	rows := make([]leanRow, 0, len(syms))
	for _, sym := range syms {
		rows = append(rows, leanRow{
			Code:      s.codes.codeFor(sym),
			Name:      sym.Name,
			Kind:      sym.Kind,
			File:      sym.File,
			StartLine: sym.StartLine,
			EndLine:   sym.EndLine,
		})
	}
	body, err := json.MarshalIndent(map[string]any{
		"codec":   "lean",
		"count":   len(rows),
		"symbols": rows,
		"hint":    "call mcp__keeba__expand(code) for full sig+doc on any row",
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

func notCompiledResponse() rpcResponse {
	body, _ := json.Marshal(map[string]any{
		"error": "no symbol graph in this directory — run `keeba compile` first",
	})
	return rpcResponse{Result: map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(body),
		}},
	}}
}
