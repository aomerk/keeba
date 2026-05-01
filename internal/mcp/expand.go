package mcp

import (
	"encoding/json"
	"strings"
)

// expandArgs is the argument shape for the expand tool.
type expandArgs struct {
	Code string `json:"code"`
}

// toolExpand resolves a session-scoped symbol code (s1, s42, ...) back
// to the full Symbol struct: name, kind, file, start_line, end_line,
// signature, doc, receiver, language. Pairs with the lean codec mode
// (banger phase 15 / L2): tools return codes + minimal metadata,
// agent calls expand only when it needs sig/doc/receiver detail.
//
// Codes are server-lifetime stable, so an agent that received "s42"
// from find_def in turn 1 can still expand it in turn 5. Cache-warm.
func (s *Server) toolExpand(raw json.RawMessage) rpcResponse {
	if s.codes == nil {
		return rpcResponse{Error: &rpcError{Code: -32603, Message: "codetable not initialized"}}
	}
	var a expandArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if strings.TrimSpace(a.Code) == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "code is required"}}
	}
	sym, ok := s.codes.resolve(a.Code)
	if !ok {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "unknown code: " + a.Code + " (codes are session-scoped; was the server restarted?)"}}
	}
	body, err := json.MarshalIndent(map[string]any{
		"code":   a.Code,
		"symbol": sym,
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
