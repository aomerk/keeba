package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/bench"
	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/llm"
)

func newBenchCmd() *cobra.Command {
	var (
		questionsFile      string
		rawPaths           []string
		topK               int
		out                string
		llmProvider        string
		maxRawChars        int
		encodingSpec       string
		encodingGrid       bool
		encodingGridByType bool
		writeConfig        bool
		mcpRepo            string
	)
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Benchmark wiki vs raw sources.",
		Long: `Benchmark wiki vs raw sources.

By default, runs the byte-count bench: a sanity-check that compares the
input volume an LLM would have to ingest in each mode. Cheap, no API key.

Pass --llm anthropic to run the real evaluator: each question is answered
twice (wiki context vs raw context) by Claude. Token counts come from the
API response, and the model self-rates confidence 1-5. Requires
ANTHROPIC_API_KEY to be set.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}

			// --mcp short-circuits the wiki bench: compile the target repo,
			// run the symbol-graph MCP query suite in-process, write a
			// receipt-shaped markdown table. Independent path — none of the
			// wiki / encoding flags below apply.
			if mcpRepo != "" {
				return runMCPBench(cmd, mcpRepo, out)
			}

			qs := bench.DefaultCodeQuestions
			if questionsFile != "" {
				qs, err = readQuestions(questionsFile)
				if err != nil {
					return err
				}
			}

			if llmProvider != "" {
				return runLLMBench(cmd, cfg, qs, rawPaths, topK, maxRawChars, out, llmProvider)
			}

			rep, err := bench.Run(cfg, qs, rawPaths, topK)
			if err != nil {
				return err
			}
			md := bench.Markdown(rep)

			var encRep bench.EncodingReport
			if encodingSpec != "" {
				encRep, err = bench.RunEncoding(cfg, encodingSpec)
				if err != nil {
					return fmt.Errorf("bench --encoding %q: %w", encodingSpec, err)
				}
				md += "\n" + bench.MarkdownEncoding(encRep)
			}

			var gridRep bench.GridReport
			if encodingGrid {
				gridRep, err = bench.RunEncodingGrid(cfg)
				if err != nil {
					return fmt.Errorf("bench --encoding-grid: %w", err)
				}
				md += "\n" + bench.MarkdownEncodingGrid(gridRep)
			}

			var typedGrid bench.TypedGridReport
			if encodingGridByType {
				typedGrid, err = bench.RunEncodingGridByType(cfg)
				if err != nil {
					return fmt.Errorf("bench --encoding-grid-by-type: %w", err)
				}
				md += "\n" + bench.MarkdownEncodingGridByType(typedGrid)
			}

			if out == "" {
				out = filepath.Join(cfg.WikiRoot, "_bench",
					rep.When.Format("2006-01-02-1504")+".md")
			}
			if err := writeBench(out, []byte(md)); err != nil {
				return err
			}
			rel, _ := filepath.Rel(cfg.WikiRoot, out)
			summary := fmt.Sprintf("keeba: %.1f× cheaper, %.1f× faster (%d questions; byte-count mode)",
				safeInverse(rep.RatioTokens()), safeInverse(rep.RatioWall()), rep.N)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), summary)
			if encodingSpec != "" && encRep.Ratio() > 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"keeba: encoding %s — %.2f× compression on %d pages (%d → %d chars)\n",
					encRep.Pipeline, encRep.Ratio(), len(encRep.Pages), encRep.TotalRaw, encRep.TotalEnc)
			}
			if encodingGrid {
				if gridRep.Recommended != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(),
						"keeba: encoding-grid winner — %s (%.2f× compression)\n",
						gridRep.Recommended, gridRep.BestRatio)
				} else {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(),
						"keeba: encoding-grid — no pipeline cleared the 4.5× quality cap")
				}
			}
			if encodingGridByType && len(typedGrid.Types) > 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "keeba: encoding-grid-by-type winners:")
				for _, pt := range []bench.PageType{bench.PageTypeFunction, bench.PageTypeEntity, bench.PageTypeNarrative} {
					grid, ok := typedGrid.Types[pt]
					if !ok {
						continue
					}
					if grid.Recommended != "" {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(),
							"  %-10s (%d pages): %s — %.2f×\n",
							pt, typedGrid.PageCounts[pt], grid.Recommended, grid.BestRatio)
					} else {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(),
							"  %-10s (%d pages): no winner under cap\n",
							pt, typedGrid.PageCounts[pt])
					}
				}
				if writeConfig {
					enc := config.EncodingConfig{
						Function:  pipelineSpec(typedGrid, bench.PageTypeFunction),
						Entity:    pipelineSpec(typedGrid, bench.PageTypeEntity),
						Narrative: pipelineSpec(typedGrid, bench.PageTypeNarrative),
					}
					if err := cfg.SaveEncoding(enc); err != nil {
						return fmt.Errorf("save encoding to keeba.config.yaml: %w", err)
					}
					_, _ = fmt.Fprintln(cmd.OutOrStdout(),
						"keeba: wrote winners to keeba.config.yaml (encoding.{function,entity,narrative})")
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", rel)
			return nil
		},
	}
	cmd.Flags().StringVar(&questionsFile, "questions", "", "path to a newline-delimited question list (overrides defaults)")
	cmd.Flags().StringSliceVar(&rawPaths, "raw", nil, "raw source paths to compare against (relative to wiki root or absolute)")
	cmd.Flags().IntVarP(&topK, "top-k", "k", 5, "BM25 top-k for wiki mode")
	cmd.Flags().StringVar(&out, "out", "", "output markdown path (default: <wiki>/_bench/<date>.md)")
	cmd.Flags().StringVar(&llmProvider, "llm", "", "LLM evaluator (anthropic) — runs the real wiki-vs-raw answer benchmark")
	cmd.Flags().IntVar(&maxRawChars, "max-raw-chars", 100000, "cap on raw-mode context size (chars)")
	cmd.Flags().StringVar(&encodingSpec, "encoding", "",
		"encoding pipeline to also benchmark on the wiki (e.g. \"glossary,structural-card\"). "+
			"Reports per-page compression after the standard wiki-vs-raw run.")
	cmd.Flags().BoolVar(&encodingGrid, "encoding-grid", false,
		"run every candidate encoding pipeline and report the winner (respects the 4× quality cliff per plan §10).")
	cmd.Flags().BoolVar(&encodingGridByType, "encoding-grid-by-type", false,
		"partition wiki pages by detected page-type (function / entity / narrative) and run the grid per partition.")
	cmd.Flags().BoolVar(&writeConfig, "write-config", false,
		"after --encoding-grid-by-type, persist the per-type winners to keeba.config.yaml (encoding.{function,entity,narrative}).")
	cmd.Flags().StringVar(&mcpRepo, "mcp", "",
		"path to a code repo to bench against the symbol-graph MCP surface. "+
			"Compiles the repo, runs find_def / search_symbols / grep_symbols / find_callers / "+
			"tests_for / summary, and writes a receipt-shaped markdown report. "+
			"Independent of the wiki bench above — other flags are ignored.")
	return cmd
}

// runMCPBench compiles the target repo, runs the default MCP query
// suite in-process, and writes the markdown report to outPath (or a
// default under bench/results/).
func runMCPBench(cmd *cobra.Command, repoPath, outPath string) error {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", repoPath, err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return fmt.Errorf("--mcp %q: not a directory", repoPath)
	}
	rep, err := bench.RunMCPBench(abs, nil)
	if err != nil {
		return fmt.Errorf("mcp bench: %w", err)
	}
	md := bench.MarkdownMCPBench(rep)

	if outPath == "" {
		outPath = filepath.Join("bench", "results",
			filepath.Base(abs)+"-"+rep.When.Format("2006-01-02-1504")+".md")
	}
	if err := writeBench(outPath, []byte(md)); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"keeba: %s — %d symbols, %d edges, %.1f× cheaper across %d queries (compile %d ms)\n",
		filepath.Base(abs), rep.SymbolCount, rep.EdgeCount,
		rep.AlternativeRatio, len(rep.Queries), rep.CompileMs)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", outPath)
	return nil
}

// pipelineSpec returns the recommended pipeline name for the given
// page-type from a TypedGridReport, or "" if the type isn't present or
// has no winner under the quality cap.
func pipelineSpec(rep bench.TypedGridReport, pt bench.PageType) string {
	if rep.Types == nil {
		return ""
	}
	g, ok := rep.Types[pt]
	if !ok {
		return ""
	}
	return g.Recommended
}

func runLLMBench(
	cmd *cobra.Command,
	cfg config.KeebaConfig,
	qs []bench.Question,
	rawPaths []string,
	topK int,
	maxRawChars int,
	out string,
	provider string,
) error {
	if provider != "anthropic" {
		return fmt.Errorf("only --llm anthropic is wired today; got %q", provider)
	}
	eval, err := llm.NewAnthropic()
	if err != nil {
		return err
	}
	if eval == nil {
		return fmt.Errorf("ANTHROPIC_API_KEY not set; either export the key or drop --llm")
	}
	rep, err := bench.RunLLM(cmd.Context(), cfg, eval, qs, rawPaths, topK, maxRawChars)
	if err != nil {
		return err
	}
	md := bench.MarkdownLLM(rep)
	if out == "" {
		out = filepath.Join(cfg.WikiRoot, "_bench",
			rep.When.Format("2006-01-02-1504")+"-llm.md")
	}
	if err := writeBench(out, []byte(md)); err != nil {
		return err
	}
	rel, _ := filepath.Rel(cfg.WikiRoot, out)
	summary := fmt.Sprintf(
		"keeba (LLM, %s/%s): %.1f× cheaper input, %.1f× faster, conf wiki=%.1f vs raw=%.1f (%d questions)",
		rep.Provider, rep.Model,
		safeInverse(rep.RatioInputTokens()), safeInverse(rep.RatioWall()),
		rep.AvgWikiConfidence(), rep.AvgRawConfidence(),
		len(rep.Rows),
	)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), summary)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", rel)
	return nil
}

func writeBench(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readQuestions(path string) ([]bench.Question, error) {
	f, err := os.Open(path) //nolint:gosec // user-provided
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var qs []bench.Question
	s := bufio.NewScanner(f)
	for i := 1; s.Scan(); i++ {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		qs = append(qs, bench.Question{
			ID:   fmt.Sprintf("q%d", i),
			Text: line,
		})
	}
	return qs, s.Err()
}

func safeInverse(r float64) float64 {
	if r <= 0 {
		return 0
	}
	return 1 / r
}
