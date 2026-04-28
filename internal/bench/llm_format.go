package bench

import (
	"fmt"
	"strings"
)

// MarkdownLLM renders an LLMReport as a stable markdown document for
// committing to wiki/_bench/<date>-llm.md.
func MarkdownLLM(r LLMReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Bench (LLM) %s\n\n", r.When.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&sb, "> %d questions, evaluator %s/%s. Wiki mode passes BM25 top-k chunks; raw mode passes a truncated dump of the configured raw paths. Token counts are reported by the API response, not estimated.\n\n",
		len(r.Rows), r.Provider, r.Model)

	fmt.Fprintf(&sb, "## Headline\n\n")
	if r.RatioInputTokens() > 0 {
		fmt.Fprintf(&sb, "- **%.1f× cheaper** in input tokens (%d vs %d).\n",
			1/r.RatioInputTokens(), r.WikiSum.InputTokens, r.RawSum.InputTokens)
	}
	if r.RatioWall() > 0 {
		fmt.Fprintf(&sb, "- **%.1f× faster** in wall time (%s vs %s).\n",
			1/r.RatioWall(), r.WikiSum.Wall, r.RawSum.Wall)
	}
	fmt.Fprintf(&sb, "- Confidence: wiki avg **%.1f/5**, raw avg **%.1f/5**.\n",
		r.AvgWikiConfidence(), r.AvgRawConfidence())
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Per-question")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "| Question | Wiki in/out | Raw in/out | Wiki conf | Raw conf | Wiki wall | Raw wall |")
	fmt.Fprintln(&sb, "|---|---|---|---|---|---|---|")
	for _, row := range r.Rows {
		fmt.Fprintf(&sb, "| %s | %d/%d | %d/%d | %d/5 | %d/5 | %s | %s |\n",
			truncate(row.Question.Text, 50),
			row.Wiki.InputTokens, row.Wiki.OutputTokens,
			row.Raw.InputTokens, row.Raw.OutputTokens,
			row.Wiki.Confidence, row.Raw.Confidence,
			row.Wiki.Wall, row.Raw.Wall,
		)
	}
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Sample answers")
	fmt.Fprintln(&sb)
	for i, row := range r.Rows {
		fmt.Fprintf(&sb, "### Q%d. %s\n\n", i+1, row.Question.Text)
		fmt.Fprintln(&sb, "**Wiki-mode answer:**")
		fmt.Fprintln(&sb)
		fmt.Fprintf(&sb, "> %s\n\n", indentQuote(row.Wiki.Text))
		fmt.Fprintln(&sb, "**Raw-mode answer:**")
		fmt.Fprintln(&sb)
		fmt.Fprintf(&sb, "> %s\n\n", indentQuote(row.Raw.Text))
	}

	fmt.Fprintln(&sb, "## Sources")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "- `keeba bench --llm anthropic` run on the configured corpus.")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## See Also")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "- [[index]]")
	return sb.String()
}

func indentQuote(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n> ")
}
