package mcp

import (
	"fmt"
	"sync"

	"github.com/aomerk/keeba/internal/symbol"
)

// codeTable is the server's session-scoped symbol→code map. Used by
// the lean codec (banger phase 15 / L2): tools return interned codes
// instead of repeating full sig+doc per row, agent calls
// `mcp__keeba__expand(code)` to dereference when it actually needs the
// detail. Saves bytes on the wire when the agent reasons over a small
// number of distinct symbols across multiple tool calls (the typical
// agent-loop shape).
//
// Code stability across calls within one MCP server lifetime is
// load-bearing: Claude's prompt cache hits identical prefixes, and
// stable codes mean a symbol referenced in turn 1 keeps the same code
// in turn 5. The cache stays warm.
//
// Concurrent-safe — the MCP server fields tools/call requests
// serially today, but the contract should hold under any future
// concurrent dispatch path.
type codeTable struct {
	mu      sync.RWMutex
	bySig   map[string]string // file:line → "s42"
	entries []symbol.Symbol   // ordered by allocation
	byCode  map[string]int    // "s42" → index into entries
}

// newCodeTable builds an empty allocator.
func newCodeTable() *codeTable {
	return &codeTable{
		bySig:  map[string]string{},
		byCode: map[string]int{},
	}
}

// codeFor returns the code for sym, allocating a new one on first
// sight. Idempotent: same symbol identity always yields the same code
// for the lifetime of this codeTable.
func (ct *codeTable) codeFor(sym symbol.Symbol) string {
	key := codeKey(sym)
	ct.mu.RLock()
	if c, ok := ct.bySig[key]; ok {
		ct.mu.RUnlock()
		return c
	}
	ct.mu.RUnlock()

	ct.mu.Lock()
	defer ct.mu.Unlock()
	// Re-check under write lock — another caller may have allocated
	// the code between our RUnlock and Lock.
	if c, ok := ct.bySig[key]; ok {
		return c
	}
	c := fmt.Sprintf("s%d", len(ct.entries)+1)
	ct.bySig[key] = c
	ct.byCode[c] = len(ct.entries)
	ct.entries = append(ct.entries, sym)
	return c
}

// resolve returns the symbol for code, or false if the code wasn't
// allocated by this table. Used by the expand tool.
func (ct *codeTable) resolve(code string) (symbol.Symbol, bool) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	idx, ok := ct.byCode[code]
	if !ok {
		return symbol.Symbol{}, false
	}
	return ct.entries[idx], true
}

// codeKey is the canonical identity key. file + start_line uniquely
// identifies a symbol; signature can change as the agent edits code,
// but the location persists across re-extracts (until the symbol
// moves). For a long session, that's stable enough.
func codeKey(s symbol.Symbol) string {
	return fmt.Sprintf("%s:%d", s.File, s.StartLine)
}
