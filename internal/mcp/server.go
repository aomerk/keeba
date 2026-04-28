// Package mcp implements a minimal Model Context Protocol (MCP) server over
// stdio. v0.1 ships exactly one tool: query_documentation, backed by the
// BM25 index.
//
// MCP spec: https://spec.modelcontextprotocol.io/. We support the subset
// Claude Code, Cursor, and Codex actually call: initialize, tools/list,
// tools/call. Notifications are accepted and discarded.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/search"
)

const protocolVersion = "2024-11-05"

// rpcRequest is a JSON-RPC 2.0 request envelope.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Server is a stdio MCP server backed by a BM25 search index.
type Server struct {
	cfg     config.KeebaConfig
	idx     *search.Index
	Version string // surfaced as serverInfo.version on initialize
}

// New builds the BM25 index up-front so /tools/call queries are fast.
// The default Version is "dev"; CLI callers should set it from cli.Version.
func New(cfg config.KeebaConfig) (*Server, error) {
	idx, err := search.Build(cfg)
	if err != nil {
		return nil, fmt.Errorf("build index: %w", err)
	}
	return &Server{cfg: cfg, idx: idx, Version: "dev"}, nil
}

// Serve reads JSON-RPC frames from r (one JSON object per line, per MCP's
// stdio transport) and writes responses to w. Returns when r reaches EOF or
// ctx is canceled.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	enc := json.NewEncoder(w)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := br.ReadBytes('\n')
		if err == io.EOF {
			if len(line) == 0 {
				return nil
			}
		} else if err != nil {
			return err
		}
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}
		if req.ID == nil {
			// Notification — accept silently.
			continue
		}
		resp := s.dispatch(req)
		resp.JSONRPC = "2.0"
		resp.ID = req.ID
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

func (s *Server) dispatch(req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		v := s.Version
		if v == "" {
			v = "dev"
		}
		return rpcResponse{Result: map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo": map[string]string{
				"name":    "keeba",
				"version": v,
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		}}
	case "tools/list":
		return rpcResponse{Result: map[string]any{
			"tools": []map[string]any{{
				"name":        "query_documentation",
				"description": "Search the keeba-managed wiki via BM25. Returns up to 10 matching pages with title, slug, score, and snippet.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Free-text search query.",
						},
						"top_k": map[string]any{
							"type":        "integer",
							"description": "Number of results to return (default 5).",
							"default":     5,
						},
					},
					"required": []string{"query"},
				},
			}},
		}}
	case "tools/call":
		return s.toolsCall(req.Params)
	default:
		return rpcResponse{Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}}
	}
}

type callParams struct {
	Name      string `json:"name"`
	Arguments struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	} `json:"arguments"`
}

func (s *Server) toolsCall(raw json.RawMessage) rpcResponse {
	var p callParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad params: " + err.Error()}}
	}
	if p.Name != "query_documentation" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}}
	}
	if p.Arguments.Query == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "query is required"}}
	}
	k := p.Arguments.TopK
	if k <= 0 {
		k = 5
	}
	if k > 10 {
		k = 10
	}
	hits := s.idx.Query(p.Arguments.Query, k)
	body, _ := json.MarshalIndent(hits, "", "  ")
	return rpcResponse{Result: map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(body),
		}},
	}}
}
