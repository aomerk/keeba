package bench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/mcp"
	"github.com/aomerk/keeba/internal/symbol"
)

// MCPQuery is one row in the bench fixture: a tool call with concrete
// arguments. The bench drives each row through the in-process MCP server
// and records returned bytes, alternative bytes, hit count, and wall time.
type MCPQuery struct {
	Label string         `json:"label"`
	Tool  string         `json:"tool"`
	Args  map[string]any `json:"args"`
}

// MCPQueryResult is the recorded outcome of one query.
type MCPQueryResult struct {
	Label            string `json:"label"`
	Tool             string `json:"tool"`
	DurationMs       int64  `json:"duration_ms"`
	BytesReturned    int    `json:"bytes_returned"`
	BytesAlternative int    `json:"bytes_alternative"`
	HitsCount        int    `json:"hits_count"`
	Error            string `json:"error,omitempty"`
}

// MCPReport summarizes one full bench run against a target repository.
// Powers the README "verified at scale" table — the missing artifact
// the MCP-savings claims have been waiting for.
type MCPReport struct {
	RepoPath         string            `json:"repo_path"`
	When             time.Time         `json:"when"`
	SymbolCount      int               `json:"symbol_count"`
	EdgeCount        int               `json:"edge_count"`
	FileCount        int               `json:"file_count"`
	IndexBytes       int64             `json:"index_bytes"`
	CompileMs        int64             `json:"compile_ms"`
	Queries          []MCPQueryResult  `json:"queries"`
	TotalReturned    int64             `json:"total_returned"`
	TotalAlternative int64             `json:"total_alternative"`
	AlternativeRatio float64           `json:"alternative_ratio"`
	TokensSaved      int64             `json:"tokens_saved"`
	GoVersion        string            `json:"go_version,omitempty"`
	Extra            map[string]string `json:"extra,omitempty"`
}

// DefaultMCPQueries is the fixture suite we run against every target. The
// queries are intentionally generic — names like "main" / "http" hit any
// non-trivial Go repo, regex patterns target idioms found everywhere.
// Future iterations may take a JSON file via --queries.
var DefaultMCPQueries = []MCPQuery{
	{Label: "find_def main", Tool: "find_def", Args: map[string]any{"name": "main"}},
	{Label: "find_def Run", Tool: "find_def", Args: map[string]any{"name": "Run"}},
	{Label: "search_symbols 'http handler'", Tool: "search_symbols", Args: map[string]any{"query": "http handler", "limit": 10}},
	{Label: "search_symbols 'config load'", Tool: "search_symbols", Args: map[string]any{"query": "config load", "limit": 10}},
	{Label: "grep_symbols 'os.Getenv' (literal)", Tool: "grep_symbols", Args: map[string]any{"pattern": "os.Getenv", "literal": true, "limit": 25}},
	{Label: "grep_symbols 'context.Context' (literal)", Tool: "grep_symbols", Args: map[string]any{"pattern": "context.Context", "literal": true, "limit": 25}},
	{Label: "find_callers main", Tool: "find_callers", Args: map[string]any{"name": "main", "limit": 25}},
	{Label: "find_refs Block", Tool: "find_refs", Args: map[string]any{"name": "Block", "limit": 25}},
	{Label: "tests_for Run", Tool: "tests_for", Args: map[string]any{"name": "Run"}},
	{Label: "summary cmd/", Tool: "summary", Args: map[string]any{"file": "cmd/", "limit": 50}},
}

// RunMCPBench compiles the repo at repoPath if needed, instantiates an
// in-process MCP server, and drives the queries through it. Wiki search
// is allowed to be empty — we point cfg.WikiRoot at the repo so the
// symbol graph loads from the same root, and any non-conforming markdown
// in the tree is just absent from the wiki index.
func RunMCPBench(repoPath string, queries []MCPQuery) (MCPReport, error) {
	if len(queries) == 0 {
		queries = DefaultMCPQueries
	}
	rep := MCPReport{
		RepoPath: repoPath,
		When:     time.Now().UTC(),
	}

	// Compile if .keeba/symbols.json is missing or stale-ish. We
	// always compile fresh — running the bench twice on the same repo
	// should produce the same index size, and a half-stale index would
	// make the per-query latency noisy.
	t0 := time.Now()
	idx, err := symbol.Compile(repoPath, repoPath)
	if err != nil {
		return rep, fmt.Errorf("compile: %w", err)
	}
	rep.CompileMs = time.Since(t0).Milliseconds()
	rep.SymbolCount = idx.NumSymbols
	rep.EdgeCount = idx.NumEdges
	rep.FileCount = idx.NumFiles

	if size, err := indexFileSize(repoPath); err == nil {
		rep.IndexBytes = size
	}

	cfg := config.Defaults()
	cfg.WikiRoot = repoPath
	srv, err := mcp.New(cfg)
	if err != nil {
		return rep, fmt.Errorf("server: %w", err)
	}

	for _, q := range queries {
		before := i64(srv.Stats().Snapshot()["bytes_alternative"])
		row := runOneMCPQuery(srv, q)
		after := i64(srv.Stats().Snapshot()["bytes_alternative"])
		if delta := int(after - before); delta > 0 {
			row.BytesAlternative = delta
		}
		rep.Queries = append(rep.Queries, row)
	}

	// Pull totals from the server's own session stats — that's the
	// authoritative number agents see, so the bench should match.
	snap := srv.Stats().Snapshot()
	rep.TotalReturned = i64(snap["bytes_returned"])
	rep.TotalAlternative = i64(snap["bytes_alternative"])
	if rep.TotalReturned > 0 {
		rep.AlternativeRatio = float64(rep.TotalAlternative) / float64(rep.TotalReturned)
	}
	rep.TokensSaved = i64(snap["tokens_saved"])

	return rep, nil
}

// runOneMCPQuery drives a single query through Server.Serve over an
// in-memory pipe and records the response size, latency, and a
// best-effort hit-count parse.
func runOneMCPQuery(srv *mcp.Server, q MCPQuery) MCPQueryResult {
	res := MCPQueryResult{Label: q.Label, Tool: q.Tool}

	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": q.Tool, "arguments": q.Args},
	})
	if err != nil {
		res.Error = "marshal: " + err.Error()
		return res
	}

	in := bytes.NewReader(append(req, '\n'))
	out := &bytes.Buffer{}
	t0 := time.Now()
	if err := srv.Serve(context.Background(), in, out); err != nil {
		res.Error = "serve: " + err.Error()
		res.DurationMs = time.Since(t0).Milliseconds()
		return res
	}
	res.DurationMs = time.Since(t0).Milliseconds()

	// Parse the response: standard MCP envelope → result.content[0].text
	// for symbol-graph tools, plus an error path. Hit count is per-tool
	// best-effort — different tools name the count field differently.
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if !scanner.Scan() {
		res.Error = "no response"
		return res
	}
	var envelope struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
		res.Error = "decode: " + err.Error()
		return res
	}
	if envelope.Error != nil {
		res.Error = envelope.Error.Message
		return res
	}
	text := mcpResponseText(envelope.Result)
	res.BytesReturned = len(text)
	res.HitsCount = parseHitCount(text)
	return res
}

// mcpResponseText pulls result.content[0].text out of an MCP envelope.
func mcpResponseText(result map[string]any) string {
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		return ""
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		return ""
	}
	t, _ := first["text"].(string)
	return t
}

// parseHitCount tries to extract a hit-count integer from the JSON-shaped
// text payload. Tools name it "count" (find_def, find_callers, summary,
// search_symbols, grep_symbols, tests_for) so a single field works. If
// parsing fails we return 0 — bench rows just won't claim a count.
func parseHitCount(text string) int {
	var probe struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		return 0
	}
	return probe.Count
}

// indexFileSize stats the .keeba/symbols.json the bench just compiled.
// Returns (0, ErrNotExist) if the file is missing — the caller silently
// ignores so the report still renders.
func indexFileSize(repoPath string) (int64, error) {
	p := filepath.Join(repoPath, ".keeba", "symbols.json")
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		return 0, err
	}
	return info.Size(), nil
}

func i64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

// MarkdownMCPBench renders an MCPReport as a stable markdown document
// suitable for committing to bench/results/<repo>-<date>.md and pasting
// into the README.
func MarkdownMCPBench(r MCPReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# keeba MCP bench — %s\n\n", filepath.Base(r.RepoPath))
	fmt.Fprintf(&sb, "_%s_\n\n", r.When.Format("2006-01-02 15:04:05 UTC"))

	fmt.Fprintln(&sb, "## Index")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "| Metric | Value |")
	fmt.Fprintln(&sb, "|---|---|")
	fmt.Fprintf(&sb, "| Repo | `%s` |\n", r.RepoPath)
	fmt.Fprintf(&sb, "| Files indexed | %d |\n", r.FileCount)
	fmt.Fprintf(&sb, "| Symbols | %d |\n", r.SymbolCount)
	fmt.Fprintf(&sb, "| Call edges | %d |\n", r.EdgeCount)
	fmt.Fprintf(&sb, "| Compile time | %d ms |\n", r.CompileMs)
	fmt.Fprintf(&sb, "| Index size on disk | %s |\n", humanBytes(r.IndexBytes))
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Receipt")
	fmt.Fprintln(&sb)
	if r.AlternativeRatio > 0 {
		fmt.Fprintf(&sb, "- **%.1f× cheaper** in returned bytes vs unfiltered alternative\n",
			r.AlternativeRatio)
	}
	fmt.Fprintf(&sb, "- bytes_returned: %s | bytes_alternative: %s | tokens_saved: %d\n",
		humanBytes(r.TotalReturned), humanBytes(r.TotalAlternative), r.TokensSaved)
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Per-query")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "| Query | Tool | Latency | Returned | Alternative | Hits |")
	fmt.Fprintln(&sb, "|---|---|---|---|---|---|")
	for _, q := range r.Queries {
		alt := "—"
		if q.BytesAlternative > 0 {
			alt = humanBytes(int64(q.BytesAlternative))
		}
		errMark := ""
		if q.Error != "" {
			errMark = " ⚠ " + q.Error
		}
		fmt.Fprintf(&sb, "| %s%s | %s | %d ms | %s | %s | %d |\n",
			q.Label, errMark, q.Tool, q.DurationMs, humanBytes(int64(q.BytesReturned)), alt, q.HitsCount)
	}
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Notes")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "- Receipt totals come from the in-process `session_stats` snapshot — the same number an agent reads at runtime.")
	fmt.Fprintln(&sb, "- `Alternative` per row is the byte-size sum the agent would have pulled in unfiltered to reach the same result without keeba. Each tool replays its own filter logic against the symbol graph, sums distinct file sizes via `os.Stat`, and contributes only the result set the user actually saw (bounded by limits + filters).")
	return sb.String()
}

// humanBytes formats a byte count for human readers in tables.
func humanBytes(n int64) string {
	if n < 0 {
		return "0 B"
	}
	const (
		kib = 1024
		mib = 1024 * kib
	)
	switch {
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
