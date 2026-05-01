package context

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RenderMarkdown turns a Report into a paste-ready markdown block. The
// shape is stable so users can drop it into any AI tool: headline,
// the prompt itself echoed back, then three sections (BM25, by-name,
// literals). Trailing "## Use" gives the caller a hint for how to
// frame the follow-up message.
func RenderMarkdown(r Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# keeba context\n\n")
	fmt.Fprintf(&sb, "_%s_\n\n", filepath.Base(r.RepoPath))
	fmt.Fprintf(&sb, "**Prompt:** %s\n\n", oneLine(r.Prompt))

	// BM25 section first — the most likely-relevant symbols overall.
	if len(r.BM25Hits) > 0 {
		fmt.Fprintln(&sb, "## Most relevant (BM25 over name+sig+doc)")
		fmt.Fprintln(&sb)
		for _, h := range r.BM25Hits {
			fmt.Fprintf(&sb, "- **`%s`** `%s:%d` — %s\n",
				h.Symbol.Name, h.Symbol.File, h.Symbol.StartLine, oneLine(h.Symbol.Doc))
			if h.Symbol.Signature != "" {
				fmt.Fprintf(&sb, "  ```\n  %s\n  ```\n", h.Symbol.Signature)
			}
		}
		fmt.Fprintln(&sb)
	}

	// Identifier lookups — exact-name resolutions of CamelCase /
	// snake_case tokens spotted in the prompt. Group by identifier so
	// the user can see which prompt-token landed where.
	if len(r.NameHits) > 0 {
		fmt.Fprintln(&sb, "## By name")
		fmt.Fprintln(&sb)
		grouped := map[string][]NameHit{}
		order := []string{}
		for _, h := range r.NameHits {
			if _, ok := grouped[h.Identifier]; !ok {
				order = append(order, h.Identifier)
			}
			grouped[h.Identifier] = append(grouped[h.Identifier], h)
		}
		for _, name := range order {
			fmt.Fprintf(&sb, "- **`%s`** →\n", name)
			for _, h := range grouped[name] {
				fmt.Fprintf(&sb, "  - `%s:%d` (%s) %s\n",
					h.Symbol.File, h.Symbol.StartLine, h.Symbol.Kind, h.Symbol.Signature)
			}
		}
		fmt.Fprintln(&sb)
	}

	// Literal grep — quoted strings from the prompt, with the matching
	// line snippet so the agent knows what's there before asking.
	if len(r.LiteralHits) > 0 {
		fmt.Fprintln(&sb, "## Literal hits")
		fmt.Fprintln(&sb)
		for _, h := range r.LiteralHits {
			fmt.Fprintf(&sb, "- `%s` in **`%s`** `%s:%d`\n  ```\n  %s\n  ```\n",
				h.Literal, h.Symbol.Name, h.Symbol.File, h.Line, h.Snippet)
		}
		fmt.Fprintln(&sb)
	}

	if len(r.BM25Hits)+len(r.NameHits)+len(r.LiteralHits) == 0 {
		fmt.Fprintln(&sb, "_(no symbol-graph hits — prompt has no matching identifiers, BM25 terms, or quoted literals)_")
	}

	fmt.Fprintln(&sb, "## Use")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "Paste this block + your follow-up question to your AI tool. The block grounds the agent in the repo's actual symbols + file:line locations so it doesn't have to grep.")

	out := sb.String()
	if r.MaxBytes > 0 && len(out) > r.MaxBytes {
		// Cut on a line boundary so we don't slice mid-token.
		cut := r.MaxBytes
		if nl := strings.LastIndex(out[:cut], "\n"); nl > 0 {
			cut = nl
		}
		out = out[:cut] + "\n\n_…truncated to MaxBytes_\n"
	}
	return out
}

// oneLine collapses internal newlines + trims, so signatures / doc
// strings render cleanly as table-row text.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}
