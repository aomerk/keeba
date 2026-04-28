package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/embed"
	"github.com/aomerk/keeba/internal/search"
)

func newIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "Embed every wiki page and persist a vector store under .keeba-cache/.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			emb, err := embed.NewFromEnv()
			if err != nil {
				return err
			}
			n, err := search.IndexAndPersist(cmd.Context(), cfg, emb)
			if err != nil {
				return err
			}
			path := filepath.Join(cfg.WikiRoot, search.VectorStorePath)
			rel, _ := filepath.Rel(cfg.WikiRoot, path)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"indexed %d pages with %s/%s → %s\n",
				n, emb.Provider(), emb.Model(), rel)
			return nil
		},
	}
}
