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

// MarkdownEncodingGridByType renders a TypedGridReport showing the
// per-page-type winner and side-by-side compression numbers.
func MarkdownEncodingGridByType(r TypedGridReport) string {
	var sb strings.Builder
	fmt.Fprintln(&sb, "## Encoding grid by page-type")
	fmt.Fprintln(&sb)
	if len(r.Types) == 0 {
		fmt.Fprintln(&sb, "_(no pages found in the wiki)_")
		return sb.String()
	}
	for _, pt := range pageTypeOrder {
		grid, ok := r.Types[pt]
		if !ok {
			continue
		}
		count := r.PageCounts[pt]
		fmt.Fprintf(&sb, "### %s pages (%d)\n\n", pt, count)
		if grid.Recommended != "" {
			fmt.Fprintf(&sb, "Winner: **`%s`** (%.2f× compression)\n\n", grid.Recommended, grid.BestRatio)
		} else {
			fmt.Fprintln(&sb, "_(no pipeline cleared the 4.5× quality cap)_")
			fmt.Fprintln(&sb)
		}
		fmt.Fprintln(&sb, "| Pipeline | Compression | Total raw | Total enc |")
		fmt.Fprintln(&sb, "|---|---|---|---|")
		for _, p := range grid.Reports {
			marker := ""
			if p.Pipeline == grid.Recommended {
				marker = " ⭐"
			}
			fmt.Fprintf(&sb, "| `%s`%s | %.2f× | %d | %d |\n",
				p.Pipeline, marker, p.Ratio(), p.TotalRaw, p.TotalEnc)
		}
		fmt.Fprintln(&sb)
	}
	return sb.String()
}

// MarkdownEncodingGrid renders a GridReport as a markdown section
// summarizing every candidate pipeline + the recommendation.
func MarkdownEncodingGrid(r GridReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Encoding grid (winner: `%s`)\n\n", r.Recommended)
	if r.Recommended != "" {
		fmt.Fprintf(&sb, "Pipelines compared: %d. Recommended pipeline shrinks the wiki by **%.2f×** (under the %.1f× quality cliff per plan §10).\n\n",
			len(r.Reports), r.BestRatio, CompressionCap)
	} else {
		fmt.Fprintf(&sb, "Pipelines compared: %d. No pipeline cleared the %.1f× cap — wiki may be too small or page-types mismatch.\n\n",
			len(r.Reports), CompressionCap)
	}
	fmt.Fprintln(&sb, "| Pipeline | Compression | Total raw | Total enc | Pages |")
	fmt.Fprintln(&sb, "|---|---|---|---|---|")
	for _, p := range r.Reports {
		marker := ""
		if p.Pipeline == r.Recommended {
			marker = " ⭐"
		}
		fmt.Fprintf(&sb, "| `%s`%s | %.2f× | %d | %d | %d |\n",
			p.Pipeline, marker, p.Ratio(), p.TotalRaw, p.TotalEnc, len(p.Pages))
	}
	fmt.Fprintln(&sb)
	return sb.String()
}

// MarkdownEncoding renders an EncodingReport as a markdown section
// suitable for appending to a bench output file (or printing standalone).
func MarkdownEncoding(r EncodingReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Encoding compression — `%s`\n\n", r.Pipeline)
	if r.Ratio() > 0 {
		fmt.Fprintf(&sb, "**Total: %d pages, %d → %d chars (%.2f× compression).**\n\n",
			len(r.Pages), r.TotalRaw, r.TotalEnc, r.Ratio())
	}
	if len(r.Pages) == 0 {
		fmt.Fprintln(&sb, "(no pages indexed)")
		return sb.String()
	}
	fmt.Fprintln(&sb, "| Page | Raw chars | Encoded chars | Compression |")
	fmt.Fprintln(&sb, "|---|---|---|---|")
	for _, p := range r.Pages {
		fmt.Fprintf(&sb, "| %s | %d | %d | %.2f× |\n",
			truncate(p.Slug, 60), p.RawChars, p.EncChars, p.Ratio())
	}
	fmt.Fprintln(&sb)
	return sb.String()
}
