package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

func chunkServer(t *testing.T, files map[string]string) *Server {
	t.Helper()
	root := t.TempDir()
	// Wiki bits required by search.Build (BM25 over wiki pages).
	writeFile(t, filepath.Join(root, "concepts", "auth.md"),
		validFM+"# Authentication\n\n> JWT-based session handling.\n\n## Sources\n\n## See Also\n")

	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Defaults()
	cfg.WikiRoot = root
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReadChunk_ExactRange(t *testing.T) {
	body := "line1\nline2\nline3\nline4\nline5\n"
	s := chunkServer(t, map[string]string{"src/foo.go": body})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_chunk","arguments":{"file":"src/foo.go","start_line":2,"end_line":4}}}`,
	)
	text := mcpText(t, resps[0])
	if !strings.Contains(text, "line2\nline3\nline4") {
		t.Errorf("expected lines 2-4, got %q", text)
	}
	if strings.Contains(text, "line1") || strings.Contains(text, "line5") {
		t.Errorf("range too wide, got %q", text)
	}
	if !strings.Contains(text, "src/foo.go:2-4") {
		t.Errorf("expected location header in output, got %q", text)
	}
}

func TestReadChunk_CapsAtFileEnd(t *testing.T) {
	body := "a\nb\nc\n"
	s := chunkServer(t, map[string]string{"x.txt": body})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_chunk","arguments":{"file":"x.txt","start_line":1,"end_line":99}}}`,
	)
	text := mcpText(t, resps[0])
	// 4 lines because the trailing newline produces a 4th empty line in
	// the split. End is capped at file length.
	if !strings.Contains(text, "a\nb\nc") {
		t.Errorf("expected full body, got %q", text)
	}
}

func TestReadChunk_RespectsMaxLines(t *testing.T) {
	body := strings.Repeat("line\n", 500)
	s := chunkServer(t, map[string]string{"big.txt": body})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_chunk","arguments":{"file":"big.txt","start_line":1,"end_line":500,"max_lines":10}}}`,
	)
	text := mcpText(t, resps[0])
	count := strings.Count(text, "line\n") + strings.Count(text, "line")
	// Header line + 10 body lines roughly. Just ensure we didn't return
	// 500 lines.
	if count > 50 {
		t.Errorf("max_lines=10 didn't cap output, got %d 'line' occurrences", count)
	}
}

func TestReadChunk_RejectsAbsolutePath(t *testing.T) {
	s := chunkServer(t, nil)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"read_chunk","arguments":{"file":"/etc/passwd","start_line":1,"end_line":10}}}`,
	)
	if resps[0]["error"] == nil {
		t.Error("expected error for absolute path")
	}
}

func TestReadChunk_RejectsTraversal(t *testing.T) {
	s := chunkServer(t, nil)
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"read_chunk","arguments":{"file":"../../../etc/passwd","start_line":1,"end_line":10}}}`,
	)
	if resps[0]["error"] == nil {
		t.Error("expected error for path traversal")
	}
}

func TestReadChunk_StartBeyondFileErrors(t *testing.T) {
	s := chunkServer(t, map[string]string{"tiny.txt": "one line\n"})
	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"read_chunk","arguments":{"file":"tiny.txt","start_line":99,"end_line":100}}}`,
	)
	if resps[0]["error"] == nil {
		t.Error("expected error when start_line > file length")
	}
}

func TestSafeJoin_Pinned(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		rel       string
		shouldErr bool
	}{
		{"foo.go", false},
		{"sub/dir/file.go", false},
		{"./foo.go", false},
		{"sub/../foo.go", false},
		{"../escape", true},
		{"../../escape", true},
		{"/etc/passwd", true},
	}
	for _, tc := range cases {
		t.Run(tc.rel, func(t *testing.T) {
			_, err := safeJoin(root, tc.rel)
			if tc.shouldErr && err == nil {
				t.Errorf("expected error for %q", tc.rel)
			}
			if !tc.shouldErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tc.rel, err)
			}
		})
	}
}
