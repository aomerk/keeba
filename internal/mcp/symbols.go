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
	return symbolListResponse(matches)
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
