package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/lint"
)

func newLintCmd() *cobra.Command {
	var (
		staged           bool
		singleFile       string
		format           string
		warningsAsErrors bool
	)
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Run schema lint over the wiki.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			targets, err := pickTargets(cfg, staged, singleFile)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				fmt.Fprintln(os.Stderr, "lint: no .md targets")
				return nil
			}
			result, err := lint.Run(targets, cfg)
			if err != nil {
				return err
			}
			return printResult(cmd, cfg, result, format, warningsAsErrors)
		},
	}
	cmd.Flags().BoolVar(&staged, "staged", false, "lint files staged in git")
	cmd.Flags().StringVar(&singleFile, "file", "", "lint a single file")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json")
	cmd.Flags().BoolVar(&warningsAsErrors, "warnings-as-errors", false, "exit 1 on warnings too")
	return cmd
}

func loadCfg(cmd *cobra.Command) (config.KeebaConfig, error) {
	wikiRoot, _ := cmd.Flags().GetString("wiki-root")
	return config.Load(wikiRoot)
}

func pickTargets(cfg config.KeebaConfig, staged bool, singleFile string) ([]string, error) {
	switch {
	case singleFile != "":
		return []string{singleFile}, nil
	case staged:
		return lint.StagedPages(cfg.WikiRoot)
	default:
		return lint.AllPages(cfg.WikiRoot, cfg.Lint)
	}
}

func printResult(cmd *cobra.Command, cfg config.KeebaConfig, r lint.RunResult, format string, warningsAsErrors bool) error {
	switch format {
	case "json":
		s, err := lint.FormatJSON(r.Violations, cfg.WikiRoot)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), s)
	default:
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), lint.FormatText(r.Violations, cfg.WikiRoot))
	}
	if r.Errors > 0 || (warningsAsErrors && r.Warnings > 0) {
		return errSilentFail
	}
	return nil
}
