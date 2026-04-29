package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// readChunkArgs is the argument shape for the read_chunk tool.
type readChunkArgs struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	// MaxLines caps the response so a runaway range doesn't blow the
	// agent's context. Default 200, hard cap 1000.
	MaxLines int `json:"max_lines,omitempty"`
}

// toolReadChunk returns the exact line range of a file. Designed to pair
// with find_def: the agent learns the symbol's file + line range, then
// pulls just that body — typically 30-200 lines instead of an 800-line
// `read_file` of the whole file. Locked to the wiki/repo root so it can't
// read /etc/passwd via path traversal.
func (s *Server) toolReadChunk(raw json.RawMessage) rpcResponse {
	var a readChunkArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if strings.TrimSpace(a.File) == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "file is required"}}
	}
	if a.StartLine <= 0 {
		a.StartLine = 1
	}
	if a.EndLine < a.StartLine {
		a.EndLine = a.StartLine
	}
	maxLines := a.MaxLines
	if maxLines <= 0 {
		maxLines = 200
	}
	if maxLines > 1000 {
		maxLines = 1000
	}

	root := s.cfg.WikiRoot
	abs, err := safeJoin(root, a.File)
	if err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	body, err := os.ReadFile(abs) //nolint:gosec // path is bounded by safeJoin
	if err != nil {
		return rpcResponse{Error: &rpcError{Code: -32603, Message: "read " + a.File + ": " + err.Error()}}
	}

	lines := strings.Split(string(body), "\n")
	if a.StartLine > len(lines) {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: fmt.Sprintf("start_line %d > file length %d", a.StartLine, len(lines))}}
	}
	end := a.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	if end-a.StartLine+1 > maxLines {
		end = a.StartLine + maxLines - 1
	}
	chunk := lines[a.StartLine-1 : end]

	return rpcResponse{Result: map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": fmt.Sprintf(
				"// %s:%d-%d  (%d lines, %d total in file)\n%s",
				a.File, a.StartLine, end, len(chunk), len(lines),
				strings.Join(chunk, "\n"),
			),
		}},
	}}
}

// safeJoin resolves rel against root and rejects any path that escapes
// root (defense against ../../../etc/passwd-style traversal). Returns
// the absolute, cleaned path on success.
func safeJoin(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths not allowed: %q", rel)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(rootAbs, rel)
	candidate = filepath.Clean(candidate)
	rel2, err := filepath.Rel(rootAbs, candidate)
	if err != nil || strings.HasPrefix(rel2, "..") || rel2 == ".." {
		return "", fmt.Errorf("path escapes wiki root: %q", rel)
	}
	return candidate, nil
}
