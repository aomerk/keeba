package context

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/symbol"
)

// SymTab is the dictionary built from a Report. Each unique symbol
// (keyed by file + start_line) gets a stable short code (s1, s2, ...).
// The compact renderer references symbols by code instead of repeating
// the full name + signature in every section, on the bet that the
// duplicate appearances dominate the injected-context byte cost.
//
// The codec layer is the keeba-as-LLM-bytecode framing: symbol graph
// is the dictionary, hook is the encoder, model decodes by reading
// the table once and using codes thereafter. Caveman mode for keeba's
// injected context.
type SymTab struct {
	// Entries are ordered by first appearance (BM25 hit > NameHit >
	// LiteralHit) so the table reads top-down.
	Entries []SymTabEntry
	// codeBySig maps the symbol's identity (file:start_line) to its
	// assigned code. Used by the renderer to substitute references.
	codeBySig map[string]string
}

// SymTabEntry is one row of the table — the dictionary definition for
// a code.
type SymTabEntry struct {
	Code   string        // s1, s2, ...
	Symbol symbol.Symbol // canonical metadata
}

// BuildSymTab walks a Report in (BM25 → name hits → literal hits) order
// and assigns codes to each unique symbol. Stable order = stable codes,
// which keeps Claude's prompt cache hot across turns when the symbol
// set hasn't changed.
func BuildSymTab(r Report) *SymTab {
	st := &SymTab{codeBySig: map[string]string{}}

	visit := func(s symbol.Symbol) {
		key := symKey(s)
		if _, ok := st.codeBySig[key]; ok {
			return
		}
		code := fmt.Sprintf("s%d", len(st.Entries)+1)
		st.codeBySig[key] = code
		st.Entries = append(st.Entries, SymTabEntry{Code: code, Symbol: s})
	}

	for _, h := range r.BM25Hits {
		visit(h.Symbol)
	}
	for _, h := range r.NameHits {
		visit(h.Symbol)
	}
	for _, h := range r.LiteralHits {
		visit(h.Symbol)
	}
	return st
}

// Code returns the code assigned to s, or "" if the symbol wasn't in
// the report. Renderers use the empty return as a signal to fall back
// to the full name (defensive — shouldn't happen if the renderer walks
// the same Report the table was built from).
func (st *SymTab) Code(s symbol.Symbol) string {
	if st == nil {
		return ""
	}
	return st.codeBySig[symKey(s)]
}

// symKey is the canonical identity key. file + start_line uniquely
// identifies a Go symbol (overload-by-name is rare and resolves
// differently on file:line). Edge case: regex extractors can produce
// near-duplicate entries for malformed source — we tolerate the rare
// dup, the receipt is correct either way.
func symKey(s symbol.Symbol) string {
	return s.File + ":" + symLineStr(s.StartLine)
}

func symLineStr(n int) string {
	// Cheap itoa to avoid pulling strconv into hot paths. The line
	// numbers we see are 1..few-tens-of-thousands; a couple branches
	// suffice.
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// RenderTable emits the lean dictionary — one line per symbol with
// name + file:line + kind only. Signature and doc are deliberately
// omitted: agents that need them call `find_def` / `read_chunk` for
// the specific symbol, paying the byte cost only on demand.
//
// We tried fat dictionary entries (sig + truncated doc inline) and the
// codec lost on real prompts where BM25 and literal-hit symbols are
// disjoint — each symbol referenced once, per-entry overhead exceeded
// per-section savings. Lean entries flip the math: keeba inlines the
// minimum the agent needs to know a symbol exists, agent fetches
// details only when reasoning needs them.
func (st *SymTab) RenderTable() string {
	if st == nil || len(st.Entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Symbol table (lean — call `find_def` / `read_chunk` for sig+body)\n\n")
	for _, e := range st.Entries {
		sym := e.Symbol
		fmt.Fprintf(&sb, "- `%s` `%s` @ `%s:%d`",
			e.Code, sym.Name, sym.File, sym.StartLine)
		if sym.Kind != "" && sym.Kind != "function" {
			// "function" is the modal kind on Go repos; suppress for
			// noise. Surface non-default kinds (method, type,
			// interface, const, var) which carry real signal.
			fmt.Fprintf(&sb, " (%s)", sym.Kind)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderRefList collapses a slice of symbols to a comma-separated list
// of codes, ordered by code-number ascending so callers get stable
// output. Symbols missing from the table fall back to full name —
// defensive against the renderer drifting from the builder.
func (st *SymTab) renderRefList(syms []symbol.Symbol) string {
	type ref struct {
		code, name string
	}
	refs := make([]ref, 0, len(syms))
	for _, s := range syms {
		if c := st.Code(s); c != "" {
			refs = append(refs, ref{code: c, name: s.Name})
		} else {
			refs = append(refs, ref{code: "", name: s.Name})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		// Codes sort numerically by stripping the "s" prefix.
		ci, cj := refs[i].code, refs[j].code
		if ci != "" && cj != "" {
			return symCodeLess(ci, cj)
		}
		return refs[i].name < refs[j].name
	})
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.code != "" {
			parts = append(parts, "`"+r.code+"`")
		} else {
			parts = append(parts, "`"+r.name+"`")
		}
	}
	return strings.Join(parts, ", ")
}

// symCodeLess returns true when "s12" < "s100" — numeric compare on
// the digits, not lexicographic. Keeps the rendered list ordered by
// table appearance.
func symCodeLess(a, b string) bool {
	// Both start with "s". Compare lengths first (shorter = lower
	// number), then content.
	a, b = a[1:], b[1:]
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}
