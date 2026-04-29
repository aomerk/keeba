package mcp

import (
	"strings"
	"testing"
)

func TestSessionStatsRecord(t *testing.T) {
	st := &SessionStats{}
	st.Record("find_def", 200, 0)
	st.Record("read_chunk", 150, 800)
	st.Record("read_chunk", 100, 800)

	snap := st.Snapshot()
	if got := snap["total_calls"].(int); got != 3 {
		t.Errorf("total_calls = %d, want 3", got)
	}
	if got := snap["bytes_returned"].(int64); got != 450 {
		t.Errorf("bytes_returned = %d, want 450", got)
	}
	if got := snap["bytes_alternative"].(int64); got != 1600 {
		t.Errorf("bytes_alternative = %d, want 1600", got)
	}
	if got := snap["bytes_saved"].(int64); got != 1150 {
		t.Errorf("bytes_saved = %d, want 1150 (1600 - 450)", got)
	}
	calls := snap["calls_by_tool"].(map[string]int)
	if calls["read_chunk"] != 2 || calls["find_def"] != 1 {
		t.Errorf("calls_by_tool = %v, want read_chunk=2, find_def=1", calls)
	}
}

func TestSessionStatsZeroCallsSummary(t *testing.T) {
	st := &SessionStats{}
	if !strings.Contains(st.SummaryLine(), "0 tool calls") {
		t.Errorf("expected 0-calls message, got %q", st.SummaryLine())
	}
}

func TestSessionStatsSummaryWithSavings(t *testing.T) {
	st := &SessionStats{}
	st.Record("read_chunk", 100, 4000)
	line := st.SummaryLine()
	if !strings.Contains(line, "1 tool calls") {
		t.Errorf("expected tool-call count, got %q", line)
	}
	if !strings.Contains(line, "saved") {
		t.Errorf("expected savings claim, got %q", line)
	}
	if !strings.Contains(line, "×") {
		t.Errorf("expected ratio with multiplier, got %q", line)
	}
}

func TestSessionStatsSummaryWithoutSavings(t *testing.T) {
	st := &SessionStats{}
	st.Record("find_def", 200, 0) // alternative=0 means we don't claim savings
	line := st.SummaryLine()
	if strings.Contains(line, "saved") {
		t.Errorf("should not claim savings when alternative=0, got %q", line)
	}
}

func TestToolSessionStatsViaRPC(t *testing.T) {
	s := chunkServer(t, map[string]string{"src/foo.go": "line1\nline2\nline3\nline4\nline5\n"})

	resps := roundTrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_chunk","arguments":{"file":"src/foo.go","start_line":1,"end_line":2}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"session_stats","arguments":{}}}`,
	)
	text := mcpText(t, resps[1])
	// session_stats reports the snapshot BEFORE its own call is
	// recorded — reading the receipt shouldn't bump the receipt — so
	// total_calls reflects everything-but-the-current-stats-query.
	if !strings.Contains(text, `"total_calls": 1`) {
		t.Errorf("expected total_calls=1 (read_chunk only; session_stats excludes itself), got %q", text)
	}
	if !strings.Contains(text, `"bytes_alternative"`) {
		t.Errorf("expected bytes_alternative field in stats, got %q", text)
	}
	if !strings.Contains(text, `"read_chunk": 1`) {
		t.Errorf("expected read_chunk in calls_by_tool, got %q", text)
	}
}

func TestRecordOnNilSessionStatsIsSafe(t *testing.T) {
	var st *SessionStats
	// Should not panic.
	st.Record("find_def", 1, 1)
	if snap := st.Snapshot(); len(snap) != 0 {
		t.Errorf("nil snapshot should be empty, got %v", snap)
	}
}
