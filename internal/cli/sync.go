package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/scaffold"
)

func newSyncCmd() *cobra.Command {
	var fromRepo string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Re-import a source repo's docs, preserving any wiki pages you've edited.",
		Long: `Sync refreshes wiki pages that were created by ` + "`keeba init --from-repo`" + `.

For each source file (README.md, CLAUDE.md, ARCHITECTURE.md, docs/**, doc/**,
nested <subdir>/README.md):

  - Destination doesn't exist  → write fresh (new file in source)
  - Destination is pristine    → overwrite with new import
  - Destination was edited     → leave alone, log under "edited"

A page is "pristine" when its frontmatter carries
` + "`keeba_pristine_hash`" + ` and the body's hash still matches that value.
Editing the body (or removing the hash) takes the page off the sync path —
that's the escape hatch for "I don't want keeba touching this anymore".

Manual pages (created without ` + "`--from-repo`" + `) have no hash and are
always preserved.

Idempotent. Safe to run on every commit.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fromRepo == "" {
				return fmt.Errorf("--from-repo is required")
			}
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			repoAbs, err := filepath.Abs(fromRepo)
			if err != nil {
				return err
			}
			res, err := scaffold.SyncFromRepoWithEncoding(cfg.WikiRoot, repoAbs, "", cfg.Encoding)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"sync from %s: %d updated, %d preserved (edited)\n",
				repoAbs, len(res.Imported), len(res.Edited))
			for _, s := range res.Imported {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  ↻ %s\n", s)
			}
			for _, s := range res.Edited {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  ✋ %s (skipped — locally edited)\n", s)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromRepo, "from-repo", "", "source repo to sync from (required)")
	return cmd
}
