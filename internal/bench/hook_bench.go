package bench

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aomerk/keeba/internal/context"
)

// HookPrompt is one row in the codec A/B panel — a prompt label
// (descriptive only, never the raw prompt) and the actual prompt text.
// The label appears in committed bench output; the prompt text drives
// only the local computation and is reproduced once at the top of the
// markdown report so reviewers can verify what was tested.
type HookPrompt struct {
	Label  string `json:"label"`
	Prompt string `json:"prompt"`
}

// HookBenchRow captures one prompt's byte-by-byte comparison between
// the full and symtab codecs. Only metric counts are committed — no
// source content from the target repo leaks into the report.
type HookBenchRow struct {
	Label       string  `json:"label"`
	FullBytes   int     `json:"full_bytes"`
	SymtabBytes int     `json:"symtab_bytes"`
	SavingsPct  float64 `json:"savings_pct"`
	NameHits    int     `json:"name_hits"`
	BM25Hits    int     `json:"bm25_hits"`
	LiteralHits int     `json:"literal_hits"`
	DurationMs  int64   `json:"duration_ms"`
}

// HookBenchReport aggregates a panel run. Per-prompt rows + a single
// summary line (mean / min / max savings). The summary is the
// load-bearing claim — README and PR descriptions can lift the mean
// figure honestly because it's measured across multiple prompt shapes.
type HookBenchReport struct {
	RepoBase    string         `json:"repo_base"` // basename only — never the full path
	When        time.Time      `json:"when"`
	Rows        []HookBenchRow `json:"rows"`
	MeanSavings float64        `json:"mean_savings_pct"`
	MinSavings  float64        `json:"min_savings_pct"`
	MaxSavings  float64        `json:"max_savings_pct"`
}

// DefaultHookPrompts is the built-in panel — generic enough to
// exercise any Go codebase, representative of the prompt shapes that
// move /cost in a real Claude Code session: bug investigations,
// refactor-impact questions, test discovery, onboarding, lookup-heavy
// queries. Custom prompts go through --prompts-file (kept local).
var DefaultHookPrompts = []HookPrompt{
	{
		Label:  "lookup-heavy",
		Prompt: "where is the http handler defined and what calls it",
	},
	{
		Label:  "tests-for",
		Prompt: "what tests cover the rate limiter implementation",
	},
	{
		Label:  "literal-grep",
		Prompt: `find every place we read os.Getenv and the env var name. cite the literal "DATABASE_URL" hits`,
	},
	{
		Label:  "rename-impact",
		Prompt: "I want to rename Server type. What would break?",
	},
	{
		Label:  "onboarding",
		Prompt: "explain how a request flows from main through middleware to the database write",
	},
	{
		Label:  "config-trace",
		Prompt: "where is the config loaded and what env vars does it read",
	},
	{
		Label:  "error-trace",
		Prompt: "the service is timing out under load. What are the three most likely code paths to look at?",
	},
}

// RunHookBench drives every prompt through both codecs and returns the
// aggregated report. No file I/O on the target repo's source happens
// in this loop — only the symbol graph (`.keeba/symbols.json`) and
// any files that `context.Build`'s literal-grep needs to scan. Source
// content stays in memory; only byte counts cross the boundary.
func RunHookBench(repoPath string, prompts []HookPrompt) (HookBenchReport, error) {
	if len(prompts) == 0 {
		prompts = DefaultHookPrompts
	}
	rep := HookBenchReport{
		RepoBase: filepath.Base(repoPath),
		When:     time.Now().UTC(),
	}

	var totalSavings float64
	rep.MinSavings = 100
	rep.MaxSavings = -100

	for _, p := range prompts {
		t0 := time.Now()
		built, err := context.Build(repoPath, p.Prompt, context.Options{})
		if err != nil {
			return rep, fmt.Errorf("build %q: %w", p.Label, err)
		}
		full := context.RenderMarkdown(built)
		compact := context.RenderMarkdownCompact(built)

		row := HookBenchRow{
			Label:       p.Label,
			FullBytes:   len(full),
			SymtabBytes: len(compact),
			NameHits:    len(built.NameHits),
			BM25Hits:    len(built.BM25Hits),
			LiteralHits: len(built.LiteralHits),
			DurationMs:  time.Since(t0).Milliseconds(),
		}
		if len(full) > 0 {
			row.SavingsPct = (1 - float64(len(compact))/float64(len(full))) * 100
		}
		rep.Rows = append(rep.Rows, row)
		totalSavings += row.SavingsPct
		if row.SavingsPct < rep.MinSavings {
			rep.MinSavings = row.SavingsPct
		}
		if row.SavingsPct > rep.MaxSavings {
			rep.MaxSavings = row.SavingsPct
		}
	}
	if n := len(rep.Rows); n > 0 {
		rep.MeanSavings = totalSavings / float64(n)
	}
	return rep, nil
}

// MarkdownHookBench renders an aggregate-only report. By design this
// includes per-prompt LABELS (chosen to be descriptive but neutral)
// and BYTE COUNTS, never the prompt text or any content lifted from
// the target repo's source. The repo basename is also deliberately
// absent from the rendered markdown — committing this file from a
// private-repo bench run won't leak the repo's identity. Repo
// basename stays available on HookBenchReport for callers that want
// it (e.g., JSON mode, where the consumer is the local user).
func MarkdownHookBench(r HookBenchReport, includePrompts bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# keeba codec A/B\n\n")
	fmt.Fprintf(&sb, "_%s_\n\n", r.When.Format("2006-01-02 15:04:05 UTC"))

	fmt.Fprintln(&sb, "## Summary")
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- **Mean savings: %.1f%%** across %d prompts (full vs symtab codec on the hook output)\n",
		r.MeanSavings, len(r.Rows))
	fmt.Fprintf(&sb, "- Min: %.1f%% · Max: %.1f%%\n", r.MinSavings, r.MaxSavings)
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "Caveats:")
	fmt.Fprintln(&sb, "- This measures **hook output bytes**, not Claude session `/cost`. Bytes saved on injection don't translate 1:1 to dollars saved — extra `find_def`/`read_chunk` round-trips in lean mode may eat some of it back.")
	fmt.Fprintln(&sb, "- Generic-panel prompts. Real workloads vary; numbers are an order-of-magnitude indicator, not a guarantee.")
	fmt.Fprintln(&sb, "- Negative savings on a row mean the codec lost on that prompt shape (typically: every symbol referenced once, table overhead exceeds reference savings).")
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Per-prompt")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "| Prompt label | Full bytes | Symtab bytes | Savings | Name hits | BM25 hits | Literal hits | ms |")
	fmt.Fprintln(&sb, "|---|---|---|---|---|---|---|---|")
	for _, row := range r.Rows {
		fmt.Fprintf(&sb, "| %s | %d | %d | %.1f%% | %d | %d | %d | %d |\n",
			row.Label, row.FullBytes, row.SymtabBytes, row.SavingsPct,
			row.NameHits, row.BM25Hits, row.LiteralHits, row.DurationMs)
	}
	fmt.Fprintln(&sb)

	if includePrompts {
		fmt.Fprintln(&sb, "## Prompts (for reproducibility)")
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "_These are the panel prompts; no responses or source content from the target repo are reproduced here._")
		fmt.Fprintln(&sb)
		for _, p := range DefaultHookPrompts {
			fmt.Fprintf(&sb, "- **%s**: %s\n", p.Label, p.Prompt)
		}
		fmt.Fprintln(&sb)
	}

	fmt.Fprintln(&sb, "## Reproduce")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "```bash")
	fmt.Fprintln(&sb, "cd /path/to/your/go-repo")
	fmt.Fprintln(&sb, "keeba compile .")
	fmt.Fprintln(&sb, "keeba bench --hook-prompts $(pwd)")
	fmt.Fprintln(&sb, "```")
	return sb.String()
}
