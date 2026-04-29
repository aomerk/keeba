package mcp

import (
	"encoding/json"
	"fmt"
	"sync"
)

// SessionStats accumulates per-tool usage so the agent can ask "how much
// did keeba save me this session" via the session_stats tool. All
// counters are bytes; the receipt converts to tokens at 4 chars/token
// (the OpenAI/Anthropic rule-of-thumb already used in internal/bench).
type SessionStats struct {
	mu sync.Mutex

	// CallsByTool counts tools/call invocations per tool name.
	CallsByTool map[string]int `json:"calls_by_tool"`

	// BytesReturned is the total payload returned across every tool call.
	BytesReturned int64 `json:"bytes_returned"`

	// BytesAlternative is the size of the source the agent would have
	// pulled in unfiltered. Currently populated only for read_chunk
	// (full-file size vs chunk size); other tools contribute 0 — we
	// don't claim savings we can't measure.
	BytesAlternative int64 `json:"bytes_alternative"`
}

const charsPerToken = 4

// Record bumps the counters for one tool call. alternative is the
// hypothetical "agent reads the unfiltered source" cost, or 0 if the
// tool can't claim a measurable saving.
func (st *SessionStats) Record(tool string, returned, alternative int) {
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.CallsByTool == nil {
		st.CallsByTool = map[string]int{}
	}
	st.CallsByTool[tool]++
	st.BytesReturned += int64(returned)
	st.BytesAlternative += int64(alternative)
}

// Snapshot returns a stable copy of the counters with derived fields
// populated (token estimates, saved bytes, ratio).
func (st *SessionStats) Snapshot() map[string]any {
	if st == nil {
		return map[string]any{}
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	calls := make(map[string]int, len(st.CallsByTool))
	totalCalls := 0
	for k, v := range st.CallsByTool {
		calls[k] = v
		totalCalls += v
	}

	saved := st.BytesAlternative - st.BytesReturned
	if saved < 0 {
		saved = 0
	}
	ratio := 0.0
	if st.BytesReturned > 0 {
		ratio = float64(st.BytesAlternative) / float64(st.BytesReturned)
	}

	return map[string]any{
		"total_calls":       totalCalls,
		"calls_by_tool":     calls,
		"bytes_returned":    st.BytesReturned,
		"bytes_alternative": st.BytesAlternative,
		"bytes_saved":       saved,
		"tokens_returned":   st.BytesReturned / charsPerToken,
		"tokens_saved":      saved / charsPerToken,
		"alternative_ratio": ratio,
	}
}

// SummaryLine formats the receipt as a one-line headline suitable for
// an `mcp serve` shutdown log.
func (st *SessionStats) SummaryLine() string {
	snap := st.Snapshot()
	tot, _ := snap["total_calls"].(int)
	if tot == 0 {
		return "keeba: 0 tool calls this session"
	}
	saved, _ := snap["tokens_saved"].(int64)
	returned, _ := snap["tokens_returned"].(int64)
	ratio, _ := snap["alternative_ratio"].(float64)
	if saved == 0 {
		return fmt.Sprintf("keeba: %d tool calls, %d tokens returned", tot, returned)
	}
	return fmt.Sprintf(
		"keeba: %d tool calls, %d tokens returned vs ~%d unfiltered (%.1f× cheaper, ~%d tokens saved)",
		tot, returned, returned+saved, ratio, saved,
	)
}

// toolSessionStats exposes the live counters so an agent can render the
// receipt mid-session.
func (s *Server) toolSessionStats(_ json.RawMessage) rpcResponse {
	body, err := json.MarshalIndent(s.stats.Snapshot(), "", "  ")
	if err != nil {
		return rpcResponse{Error: &rpcError{Code: -32603, Message: err.Error()}}
	}
	return rpcResponse{Result: map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(body),
		}},
	}}
}
