package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/lint"
)

func newDriftCmd() *cobra.Command {
	var (
		singleFile       string
		format           string
		warningsAsErrors bool
	)
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Detect citation drift between wiki pages and source files.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			targets, err := driftTargets(cfg, singleFile)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				fmt.Fprintln(os.Stderr, "drift: no .md targets")
				return nil
			}
			result, err := lint.DriftTargets(targets, cfg)
			if err != nil {
				return err
			}
			return printResult(cmd, cfg, result, format, warningsAsErrors)
		},
	}
	cmd.Flags().StringVar(&singleFile, "file", "", "check a single file")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json")
	cmd.Flags().BoolVar(&warningsAsErrors, "warnings-as-errors", false, "exit 1 on warnings too")
	return cmd
}

func driftTargets(cfg config.KeebaConfig, singleFile string) ([]string, error) {
	if singleFile != "" {
		return []string{singleFile}, nil
	}
	return lint.AllPages(cfg.WikiRoot, cfg.Lint)
}
