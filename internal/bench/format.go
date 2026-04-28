package bench

import (
	"fmt"
	"strings"
)

// Markdown renders a Report as a stable markdown document suitable for
// committing to wiki/_bench/<date>.md.
func Markdown(r Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Bench %s\n\n", r.When.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&sb, "> %d questions; wiki mode reads BM25 top-k chunks, raw mode reads every text file under the configured raw paths.\n\n", r.N)

	fmt.Fprintf(&sb, "## Headline\n\n")
	if r.RatioTokens() > 0 {
		fmt.Fprintf(&sb, "- **%.1f× cheaper** in tokens (%d vs %d).\n", 1/r.RatioTokens(), r.WikiSum.Tokens, r.RawSum.Tokens)
	}
	if r.RatioWall() > 0 {
		fmt.Fprintf(&sb, "- **%.1f× faster** in wall time (%s vs %s).\n", 1/r.RatioWall(), r.WikiSum.Wall, r.RawSum.Wall)
	}
	fmt.Fprintln(&sb)

	fmt.Fprintf(&sb, "## Per-question\n\n")
	fmt.Fprintln(&sb, "| Question | Wiki tokens | Raw tokens | Wiki wall | Raw wall |")
	fmt.Fprintln(&sb, "|---|---|---|---|---|")
	for i := range r.Wiki {
		w := r.Wiki[i]
		raw := r.Raw[i]
		fmt.Fprintf(&sb, "| %s | %d | %d | %s | %s |\n",
			truncate(w.Question.Text, 60), w.Tokens, raw.Tokens, w.Wall, raw.Wall)
	}
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Sources")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "- `keeba bench` run on the configured corpus.")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## See Also")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "- [[index]]")
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
