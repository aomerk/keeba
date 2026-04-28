package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/lint"
)

func newMetaCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "meta",
		Short: "Rebuild _meta.json + _xref/<repo>.json.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			idx, err := lint.BuildMeta(cfg.WikiRoot, cfg.Lint, cfg.Drift)
			if err != nil {
				return err
			}
			data, err := lint.Marshal(idx)
			if err != nil {
				return err
			}
			out := filepath.Join(cfg.WikiRoot, "_meta.json")
			if check {
				existing, err := os.ReadFile(out)
				if err != nil {
					if os.IsNotExist(err) {
						fmt.Fprintln(os.Stderr, "_meta.json missing — run `keeba meta` and commit.")
						return errSilentFail
					}
					return err
				}
				if string(existing) != string(data) {
					fmt.Fprintln(os.Stderr, "_meta.json out of date — run `keeba meta` and commit.")
					return errSilentFail
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "_meta.json: up to date")
				return nil
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
			xref := lint.BuildXref(idx.Pages, cfg.Drift)
			n, err := lint.WriteXref(xref, cfg.WikiRoot)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote _meta.json (%d pages) and _xref/ (%d repo file(s))\n", idx.Count, n)
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "exit 1 if regenerated meta differs from on-disk")
	return cmd
}
