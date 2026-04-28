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
		questionsFile string
		rawPaths      []string
		topK          int
		out           string
		llmProvider   string
		maxRawChars   int
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
	return cmd
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
