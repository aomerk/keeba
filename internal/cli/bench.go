package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/bench"
)

func newBenchCmd() *cobra.Command {
	var (
		questionsFile string
		rawPaths      []string
		topK          int
		out           string
	)
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Benchmark wiki vs raw sources.",
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
			rep, err := bench.Run(cfg, qs, rawPaths, topK)
			if err != nil {
				return err
			}
			md := bench.Markdown(rep)

			if out == "" {
				out = filepath.Join(cfg.WikiRoot, "_bench",
					rep.When.Format("2006-01-02-1504")+".md")
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
				return err
			}
			rel, _ := filepath.Rel(cfg.WikiRoot, out)
			ratioT := rep.RatioTokens()
			ratioW := rep.RatioWall()
			summary := fmt.Sprintf("keeba: %.1f× cheaper, %.1f× faster (%d questions)",
				safeInverse(ratioT), safeInverse(ratioW), rep.N)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), summary)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", rel)
			return nil
		},
	}
	cmd.Flags().StringVar(&questionsFile, "questions", "", "path to a newline-delimited question list (overrides defaults)")
	cmd.Flags().StringSliceVar(&rawPaths, "raw", nil, "raw source paths to compare against (relative to wiki root or absolute)")
	cmd.Flags().IntVarP(&topK, "top-k", "k", 5, "BM25 top-k for wiki mode")
	cmd.Flags().StringVar(&out, "out", "", "output markdown path (default: <wiki>/_bench/<date>.md)")
	return cmd
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

// silence unused import in tests when cobra context is not used elsewhere.
var _ = time.Now
