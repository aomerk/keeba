package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

const validFM = "---\ntags: [test]\nlast_verified: 2026-04-28\nstatus: current\n---\n\n"

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func corpusServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "auth.md"),
		validFM+"# Authentication\n\n> JWT-based session handling.\n\nThe service issues JWT tokens after login.\n\n## Sources\n\n## See Also\n")
	writeFile(t, filepath.Join(root, "concepts", "billing.md"),
		validFM+"# Billing\n\n> Stripe-based recurring billing.\n\n## Sources\n\n## See Also\n")
	cfg := config.Defaults()
	cfg.WikiRoot = root
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func roundTrip(t *testing.T, s *Server, requests ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	out := &bytes.Buffer{}
	if err := s.Serve(context.Background(), in, out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		responses = append(responses, r)
	}
	return responses
}

func TestInitialize(t *testing.T) {
	s := corpusServer(t)
	resps := roundTrip(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if len(resps) != 1 {
		t.Fatalf("got %d responses", len(resps))
	}
	result := resps[0]["result"].(map[string]any)
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion: %v", result["protocolVersion"])
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "keeba" {
		t.Errorf("server name: %v", info["name"])
	}
}

func TestToolsList(t *testing.T) {
	s := corpusServer(t)
	resps := roundTrip(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := resps[0]["result"].(map[string]any)["tools"].([]any)

	gotNames := map[string]bool{}
	for _, t := range tools {
		gotNames[t.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"query_documentation", "find_def", "search_symbols", "summary"} {
		if !gotNames[want] {
			t.Errorf("missing tool %q in tools/list, got %v", want, gotNames)
		}
	}
}

func TestToolsCallReturnsHits(t *testing.T) {
	s := corpusServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"query_documentation","arguments":{"query":"JWT tokens","top_k":3}}}`,
	)
	result := resps[0]["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Authentication") {
		t.Errorf("expected auth in result: %s", text)
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	s := corpusServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"made_up","arguments":{}}}`,
	)
	if resps[0]["error"] == nil {
		t.Fatalf("expected error for unknown tool")
	}
}

func TestNotificationsIgnored(t *testing.T) {
	s := corpusServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/list"}`,
	)
	if len(resps) != 1 {
		t.Fatalf("expected only the tools/list response, got %d", len(resps))
	}
}

func TestUnknownMethod(t *testing.T) {
	s := corpusServer(t)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
	)
	rpcErr := resps[0]["error"].(map[string]any)
	if rpcErr["code"].(float64) != -32601 {
		t.Errorf("code: %v", rpcErr["code"])
	}
}
