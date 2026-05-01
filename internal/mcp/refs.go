package mcp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/symbol"
)

// findRefsArgs is the argument shape for the find_refs tool.
type findRefsArgs struct {
	Name  string   `json:"name"`
	Kinds []string `json:"kinds,omitempty"` // {"type","embed"} — empty = all
	File  string   `json:"file,omitempty"`  // path-prefix filter
	Limit int      `json:"limit,omitempty"` // default 25, max 200
}

// toolFindRefs returns every type / embed reference to a symbol from
// the precompiled ref graph. Pairs with find_callers (which covers
// calls): together they answer "what would break if I rename this?".
// find_callers alone misses type usage in fields, params, returns,
// composite literals, type assertions, and embedded types — find_refs
// closes that gap. Go-only in v1.
func (s *Server) toolFindRefs(raw json.RawMessage) rpcResponse {
	if s.live == nil {
		return notCompiledResponse()
	}
	var a findRefsArgs
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

	refs := filterRefs(s.live.RefsOf(a.Name), strings.TrimSpace(a.File), a.Kinds)

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].CallerFile != refs[j].CallerFile {
			return refs[i].CallerFile < refs[j].CallerFile
		}
		return refs[i].CallerLine < refs[j].CallerLine
	})
	if len(refs) > limit {
		refs = refs[:limit]
	}

	body, err := json.MarshalIndent(map[string]any{
		"callee": a.Name,
		"count":  len(refs),
		"refs":   refs,
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

// filterRefs applies the file-prefix and kinds filters in one pass.
// Kinds is a small allowlist — empty means "all kinds".
func filterRefs(in []symbol.RefEdge, filePrefix string, kinds []string) []symbol.RefEdge {
	kindSet := map[string]struct{}{}
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if k != "" {
			kindSet[k] = struct{}{}
		}
	}
	out := make([]symbol.RefEdge, 0, len(in))
	for _, r := range in {
		if filePrefix != "" && !strings.HasPrefix(r.CallerFile, filePrefix) {
			continue
		}
		if len(kindSet) > 0 {
			if _, ok := kindSet[r.Kind]; !ok {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// findRefsAlternative returns the file-size sum the agent would have
// pulled with `grep -rn "Name" .` + `read_file` each match. The caller
// files of the result set are exactly that match list. Bounded by the
// tool's own limit + filters so the receipt matches reality.
func (s *Server) findRefsAlternative(root string, raw json.RawMessage) int {
	if s.live == nil {
		return 0
	}
	var a findRefsArgs
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
	refs := filterRefs(s.live.RefsOf(a.Name), strings.TrimSpace(a.File), a.Kinds)
	if len(refs) > limit {
		refs = refs[:limit]
	}
	files := make([]string, 0, len(refs))
	for _, r := range refs {
		files = append(files, r.CallerFile)
	}
	return sumFileSizes(root, files)
}
