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
	"github.com/aomerk/keeba/internal/symbol"
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

// Server is a stdio MCP server backed by a BM25 wiki index plus, when
// available, a self-maintaining symbol graph (`.keeba/symbols.json`
// from `keeba compile`, kept fresh by an fsnotify watcher inside
// symbol.LiveIndex). Edits — Claude Code writes, IDE saves, `git pull`
// — re-extract the touched file in <50ms with no manual `keeba compile`
// re-run. SessionStats accumulates per-tool savings for the receipt.
type Server struct {
	cfg     config.KeebaConfig
	idx     *search.Index
	live    *symbol.LiveIndex // nil when no symbol graph is compiled yet
	stats   *SessionStats
	Version string // surfaced as serverInfo.version on initialize
}

// Stats exposes the live session counters. Useful when callers want to
// log the receipt at server shutdown.
func (s *Server) Stats() *SessionStats { return s.stats }

// LiveIndex returns the self-maintaining symbol graph, or nil if no
// graph is compiled yet. Callers (e.g. `keeba mcp serve`) should call
// this to start the watcher's Run loop in a goroutine.
func (s *Server) LiveIndex() *symbol.LiveIndex { return s.live }

// New builds the BM25 index up-front so /tools/call queries are fast,
// and tries to load a self-maintaining symbol graph from the wiki/repo
// root. The default Version is "dev"; CLI callers should set it from
// cli.Version.
func New(cfg config.KeebaConfig) (*Server, error) {
	idx, err := search.Build(cfg)
	if err != nil {
		return nil, fmt.Errorf("build index: %w", err)
	}
	srv := &Server{cfg: cfg, idx: idx, stats: &SessionStats{}, Version: "dev"}

	// Optional: load symbol graph from .keeba/symbols.json. Missing graph
	// is fine — the symbol-aware tools just respond with a hint to run
	// `keeba compile`. Corrupt graph or fsnotify init failure is real
	// and we surface it.
	live, err := loadLiveSymbols(cfg.WikiRoot)
	if err != nil {
		return nil, fmt.Errorf("load symbol graph: %w", err)
	}
	srv.live = live
	return srv, nil
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
			"tools": s.listTools(),
		}}
	case "tools/call":
		return s.toolsCall(req.Params)
	default:
		return rpcResponse{Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}}
	}
}

// callEnvelope is the bare shape of a tools/call request — name plus
// raw JSON arguments. Each tool unmarshals the arguments into its own
// typed struct (see find_def, summary).
type callEnvelope struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// listTools returns every tool the server currently exposes. Always
// includes query_documentation (BM25 wiki); find_def and summary are
// listed even when the symbol graph is missing — they respond with a
// "run `keeba compile` first" hint instead of disappearing, which keeps
// the UI in agents like Claude Code stable across compile/decompile.
func (s *Server) listTools() []map[string]any {
	tools := []map[string]any{
		{
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
		},
		{
			"name":        "find_def",
			"description": "Find the definition(s) of a symbol (function / class / type / interface) in the precompiled symbol graph. Returns file, line, signature, doc, language. O(1) lookup — instant.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Symbol name. Exact match preferred; case-insensitive substring used as fallback.",
					},
					"language": map[string]any{
						"type":        "string",
						"description": "Filter by language tag (go, py, ts, js, rs, java, kt, rb, c, cpp).",
					},
					"kind": map[string]any{
						"type":        "string",
						"description": "Filter by kind (function, method, class, type, interface, const, var).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max results (default 10, max 50).",
						"default":     10,
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "search_symbols",
			"description": "BM25-rank symbols by free-text query. Use when you have a concept ('auth handler', 'JWT validation', 'stripe webhook') but not the exact name. Returns up to 10 ranked hits with score, file:line, signature, doc. Pairs with find_def: search_symbols finds the symbol, find_def confirms its definition.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Free-text query. Identifier-aware ('auth' matches AuthMiddleware) and prose ('jwt validation' matches doc strings).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max results (default 10, max 50).",
						"default":     10,
					},
					"language": map[string]any{
						"type":        "string",
						"description": "Filter by language tag (go, py, ts, js, rs, java, kt, rb, c, cpp).",
					},
					"kind": map[string]any{
						"type":        "string",
						"description": "Filter by kind (function, method, class, type, interface, const, var).",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "find_callers",
			"description": "Return every call site of a symbol from the precompiled call graph. Pairs with find_def: find_def says 'X is here', find_callers says 'X is called from these N sites'. Lets the agent answer 'what would break if I rename X' in two MCP calls instead of a grep loop.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Bare symbol name. Exact match.",
					},
					"file": map[string]any{
						"type":        "string",
						"description": "Optional file or directory prefix to scope results.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max results (default 25, max 200).",
						"default":     25,
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "summary",
			"description": "List all symbols in a file or directory from the precompiled symbol graph. Returns name, kind, file:line, signature for each — no source bodies. Lets agents skim a file's surface area cheaply.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "File path or directory prefix (repo-relative). Empty returns the first N symbols across the repo.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max results (default 50, max 200).",
						"default":     50,
					},
				},
			},
		},
		{
			"name":        "session_stats",
			"description": "Return live counters for this MCP session: tool calls, bytes returned, bytes the agent would have pulled in unfiltered (read_chunk only), and the implied token savings. Agents and humans use this to render the 'you saved $X this session' receipt — the honest answer to 'is keeba worth installing'.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "read_chunk",
			"description": "Read an exact line range from a file. Pair with find_def: find_def gives you the symbol's file + line range; read_chunk pulls just that body. Typically 30-200 lines instead of an 800-line read_file of the whole file. Path is repo-relative; absolute paths and traversal outside the repo root are rejected.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "Repo-relative path.",
					},
					"start_line": map[string]any{
						"type":        "integer",
						"description": "1-based start line (inclusive).",
					},
					"end_line": map[string]any{
						"type":        "integer",
						"description": "1-based end line (inclusive). Capped at file length.",
					},
					"max_lines": map[string]any{
						"type":        "integer",
						"description": "Hard cap on returned lines (default 200, max 1000).",
						"default":     200,
					},
				},
				"required": []string{"file", "start_line", "end_line"},
			},
		},
	}
	return tools
}

func (s *Server) toolsCall(raw json.RawMessage) rpcResponse {
	var env callEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad params: " + err.Error()}}
	}

	var resp rpcResponse
	switch env.Name {
	case "query_documentation":
		resp = s.toolQueryDocumentation(env.Arguments)
	case "find_def":
		resp = s.toolFindDef(env.Arguments)
	case "search_symbols":
		resp = s.toolSearchSymbols(env.Arguments)
	case "find_callers":
		resp = s.toolFindCallers(env.Arguments)
	case "summary":
		resp = s.toolSummary(env.Arguments)
	case "read_chunk":
		resp = s.toolReadChunk(env.Arguments)
	case "session_stats":
		resp = s.toolSessionStats(env.Arguments)
	default:
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "unknown tool: " + env.Name}}
	}

	// Account: every tool call records its returned bytes; tools that
	// can compute a measurable savings (read_chunk vs. full file read)
	// also report the alternative size, which makes the session_stats
	// receipt an honest number rather than a vibe.
	returned, alternative := responseSize(resp), 0
	if env.Name == "read_chunk" {
		alternative = readChunkAlternative(s.cfg.WikiRoot, env.Arguments)
	}
	s.stats.Record(env.Name, returned, alternative)
	return resp
}

// responseSize returns the rough byte size of a tool response — the
// content text length, ignoring JSON-RPC envelope overhead.
func responseSize(r rpcResponse) int {
	res, ok := r.Result.(map[string]any)
	if !ok {
		return 0
	}
	content, ok := res["content"].([]map[string]string)
	if !ok {
		return 0
	}
	n := 0
	for _, c := range content {
		n += len(c["text"])
	}
	return n
}

// queryDocArgs is the argument shape for the existing BM25 wiki search.
type queryDocArgs struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

func (s *Server) toolQueryDocumentation(raw json.RawMessage) rpcResponse {
	var a queryDocArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "bad arguments: " + err.Error()}}
	}
	if a.Query == "" {
		return rpcResponse{Error: &rpcError{Code: -32602, Message: "query is required"}}
	}
	k := a.TopK
	if k <= 0 {
		k = 5
	}
	if k > 10 {
		k = 10
	}
	hits := s.idx.Query(a.Query, k)
	body, _ := json.MarshalIndent(hits, "", "  ")
	return rpcResponse{Result: map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(body),
		}},
	}}
}
