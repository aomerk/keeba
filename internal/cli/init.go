package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/scaffold"
)

func newInitCmd() *cobra.Command {
	var (
		purpose  string
		force    bool
		fromRepo string
	)
	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a fresh wiki repo (optionally seeded from an existing codebase).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			outDir := cwd
			name := filepath.Base(cwd)
			if len(args) == 1 {
				name = args[0]
				outDir = filepath.Join(cwd, name)
			}
			vars := scaffold.Defaults(name)
			if purpose != "" {
				vars.Purpose = purpose
			}
			if err := scaffold.Scaffold(outDir, vars, force); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "scaffolded %s at %s\n", name, outDir)
			if fromRepo != "" {
				// Pass empty repoName so the importer derives it from the
				// source path; that keeps citations meaningful (e.g.
				// "llm.c/README.md" rather than "my-wiki/README.md").
				res, err := scaffold.ImportFromRepo(outDir, fromRepo, "")
				if err != nil {
					return fmt.Errorf("import: %w", err)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "imported %d page(s) from %s\n", len(res.Imported), fromRepo)
				for _, slug := range res.Imported {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  + %s\n", slug)
				}
				if len(res.Skipped) > 0 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "skipped %d (already present)\n", len(res.Skipped))
				}
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "next: cd in, edit index.md / SCHEMA.md, then `keeba lint`")
			return nil
		},
	}
	cmd.Flags().StringVar(&purpose, "purpose", "", "one-sentence purpose for the wiki")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing non-empty directory")
	cmd.Flags().StringVar(&fromRepo, "from-repo", "", "seed the wiki from an existing codebase (imports README.md, CLAUDE.md, ARCHITECTURE.md, docs/**)")
	return cmd
}
