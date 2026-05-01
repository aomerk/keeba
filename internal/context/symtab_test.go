package context

import (
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/symbol"
)

func makeSym(name, file string, line int) symbol.Symbol {
	return symbol.Symbol{
		Name:      name,
		File:      file,
		StartLine: line,
		Kind:      "function",
		Signature: "func " + name + "()",
		Doc:       name + " does a thing.",
		Language:  "go",
	}
}

func TestBuildSymTab_DedupAcrossSections(t *testing.T) {
	// Same symbol appearing in BM25 + NameHits + LiteralHits should
	// land in the table once with one stable code.
	authMW := makeSym("AuthMiddleware", "auth.go", 5)
	rep := Report{
		BM25Hits: []symbol.SearchHit{
			{Symbol: authMW, Score: 18.5},
		},
		NameHits: []NameHit{
			{Identifier: "AuthMiddleware", Symbol: authMW},
		},
		LiteralHits: []LiteralHit{
			{Literal: "JWT", Symbol: authMW, Line: 7, Snippet: "jwt"},
		},
	}
	st := BuildSymTab(rep)
	if len(st.Entries) != 1 {
		t.Fatalf("want 1 dedup entry, got %d", len(st.Entries))
	}
	if st.Entries[0].Code != "s1" {
		t.Errorf("first code should be s1, got %q", st.Entries[0].Code)
	}
	if st.Code(authMW) != "s1" {
		t.Errorf("Code lookup for AuthMiddleware = %q, want s1", st.Code(authMW))
	}
}

func TestBuildSymTab_StableOrderByFirstAppearance(t *testing.T) {
	a := makeSym("A", "a.go", 1)
	b := makeSym("B", "b.go", 1)
	c := makeSym("C", "c.go", 1)
	rep := Report{
		BM25Hits: []symbol.SearchHit{
			{Symbol: c}, {Symbol: a},
		},
		NameHits: []NameHit{
			{Identifier: "B", Symbol: b},
		},
	}
	st := BuildSymTab(rep)
	want := []string{"C", "A", "B"} // BM25 first (c, a), then NameHits (b)
	for i, e := range st.Entries {
		if e.Symbol.Name != want[i] {
			t.Errorf("entry[%d]=%s, want %s", i, e.Symbol.Name, want[i])
		}
	}
}

func TestBuildSymTab_NilReport(t *testing.T) {
	st := BuildSymTab(Report{})
	if len(st.Entries) != 0 {
		t.Errorf("empty report should yield zero entries, got %d", len(st.Entries))
	}
	if st.Code(makeSym("X", "x.go", 1)) != "" {
		t.Errorf("Code on missing sym should return empty")
	}
}

func TestRenderTable_LeanFormat(t *testing.T) {
	// Lean format: name + file:line + (non-default) kind. NO sig, NO
	// doc — agent calls find_def / read_chunk to expand. The codec's
	// whole pitch is "table + on-demand details" vs "fat dictionary
	// + code references". Fat lost on real prompts (see L1 measurement
	// notes); lean wins.
	authMW := makeSym("AuthMiddleware", "auth.go", 5)
	st := &SymTab{
		Entries:   []SymTabEntry{{Code: "s1", Symbol: authMW}},
		codeBySig: map[string]string{symKey(authMW): "s1"},
	}
	out := st.RenderTable()
	for _, want := range []string{"## Symbol table", "`s1`", "AuthMiddleware", "auth.go:5"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderTable missing %q\n----\n%s", want, out)
		}
	}
	// "function" is the modal kind on Go repos — suppressed from output
	// to keep entries terse. Non-default kinds (method/type/etc.) DO
	// appear; tested separately below.
	if strings.Contains(out, "(function)") {
		t.Errorf("default kind should be suppressed, got:\n%s", out)
	}
	// Lean format MUST NOT include sig or doc — those are the bytes
	// the codec is shedding. Pin this so future edits don't quietly
	// re-bloat the table.
	if strings.Contains(out, "func AuthMiddleware()") {
		t.Errorf("lean table leaked signature:\n%s", out)
	}
	if strings.Contains(out, "does a thing") {
		t.Errorf("lean table leaked doc:\n%s", out)
	}
}

func TestRenderTable_NonDefaultKindAppears(t *testing.T) {
	method := symbol.Symbol{Name: "Run", File: "x.go", StartLine: 5, Kind: "method"}
	st := &SymTab{
		Entries:   []SymTabEntry{{Code: "s1", Symbol: method}},
		codeBySig: map[string]string{symKey(method): "s1"},
	}
	out := st.RenderTable()
	if !strings.Contains(out, "(method)") {
		t.Errorf("non-default kind should appear, got:\n%s", out)
	}
}

func TestRenderMarkdownCompact_SmallerThanFull(t *testing.T) {
	// On a real-world prompt the lean codec saves ~50% (measured on
	// risk-graph-indexer Slack-thread investigation: 6,104 → 2,931
	// bytes). On a small fixture the savings are smaller because the
	// "## Symbol table" header overhead is fixed but per-entry
	// savings scale with sig + doc length. Pin "≥30%" here as the
	// floor that any reasonable fixture should clear.
	syms := []symbol.Symbol{
		makeSym("AuthMiddleware", "auth/mw.go", 5),
		makeSym("ValidateToken", "auth/token.go", 8),
		makeSym("BillingHandler", "billing/h.go", 1),
		makeSym("StartServer", "cmd/main.go", 20),
	}
	rep := Report{
		RepoPath: "/repo",
		Prompt:   "investigate AuthMiddleware ValidateToken BillingHandler StartServer",
		BM25Hits: []symbol.SearchHit{
			{Symbol: syms[0], Score: 18.5},
			{Symbol: syms[1], Score: 15.6},
			{Symbol: syms[2], Score: 12.0},
			{Symbol: syms[3], Score: 9.0},
		},
		NameHits: []NameHit{
			{Identifier: "AuthMiddleware", Symbol: syms[0]},
			{Identifier: "ValidateToken", Symbol: syms[1]},
			{Identifier: "BillingHandler", Symbol: syms[2]},
			{Identifier: "StartServer", Symbol: syms[3]},
		},
	}
	full := RenderMarkdown(rep)
	compact := RenderMarkdownCompact(rep)
	if len(compact) >= len(full) {
		t.Fatalf("compact (%d) not smaller than full (%d)", len(compact), len(full))
	}
	saving := 1.0 - float64(len(compact))/float64(len(full))
	if saving < 0.30 {
		t.Errorf("compact saved %.1f%% (%d → %d), expected ≥30%%",
			saving*100, len(full), len(compact))
	}
}

func TestRenderMarkdownCompact_TablePresent(t *testing.T) {
	// Lean codec: table needs name + file:line + code. Sig + doc are
	// deliberately absent — agent fetches via find_def/read_chunk. Pin
	// the lean format so a "let's just put the doc back inline" PR
	// can't sneak the bloat back in.
	authMW := makeSym("AuthMiddleware", "auth.go", 5)
	rep := Report{
		BM25Hits: []symbol.SearchHit{
			{Symbol: authMW, Score: 10},
		},
	}
	out := RenderMarkdownCompact(rep)
	for _, want := range []string{
		"AuthMiddleware", // name
		"auth.go",        // file
		":5",             // line
		"`s1`",           // assigned code
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output missing %q\n----\n%s", want, out)
		}
	}
}

func TestRenderMarkdownCompact_HonorsMaxBytes(t *testing.T) {
	syms := []symbol.Symbol{
		makeSym("A", "a.go", 1),
		makeSym("B", "b.go", 1),
		makeSym("C", "c.go", 1),
	}
	hits := []symbol.SearchHit{}
	for _, s := range syms {
		hits = append(hits, symbol.SearchHit{Symbol: s})
	}
	rep := Report{
		BM25Hits: hits,
		MaxBytes: 200,
	}
	out := RenderMarkdownCompact(rep)
	if len(out) > 200+200 { // tail-marker slop
		t.Errorf("compact ignored MaxBytes: %d > 200+slop", len(out))
	}
}
