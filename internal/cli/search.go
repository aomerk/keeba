package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/embed"
	"github.com/aomerk/keeba/internal/search"
)

func newSearchCmd() *cobra.Command {
	var (
		topK   int
		format string
		vector bool
	)
	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search the wiki. BM25 by default; --vector for embedding-based.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			var hits []search.Hit
			if vector {
				emb, err := embed.NewFromEnv()
				if err != nil {
					return err
				}
				hits, err = search.VectorQuery(cmd.Context(), cfg, emb, args[0], topK)
				if err != nil {
					return err
				}
			} else {
				idx, err := search.Build(cfg)
				if err != nil {
					return err
				}
				hits = idx.Query(args[0], topK)
			}
			if format == "json" {
				b, err := json.MarshalIndent(hits, "", "  ")
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			}
			if len(hits) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no results")
				return nil
			}
			for i, h := range hits {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d. %s  (score=%.3f)\n   %s\n   %s\n\n",
					i+1, h.Title, h.Score, h.Slug, h.Snippet)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&topK, "top-k", "k", 5, "number of results to return")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json")
	cmd.Flags().BoolVar(&vector, "vector", false, "use the persisted vector store (run `keeba index` first)")
	return cmd
}
