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
		purpose string
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a fresh wiki repo.",
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
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "next: cd in, edit index.md / SCHEMA.md, then `keeba lint`")
			return nil
		},
	}
	cmd.Flags().StringVar(&purpose, "purpose", "", "one-sentence purpose for the wiki")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing non-empty directory")
	return cmd
}
