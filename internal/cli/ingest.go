package cli

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

//go:embed ingest_templates/*.md
var ingestTemplates embed.FS

// supportedIngestSources is the v0.1 set. Each maps to an embedded
// agents/<source>-ingest.md template that the user runs themselves (in
// Claude Code, Cursor, Codex, or claude.ai routine) to do the actual ingest.
var supportedIngestSources = map[string]string{
	"git":   "ingest_templates/git.md",
	"slack": "ingest_templates/slack.md",
}

func newIngestCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "ingest SOURCE",
		Short: "Print the agent prompt template for a given ingest source.",
		Long: `Print the agent prompt template that drives an ingest from SOURCE.

v0.1 ships prompt templates only — the actual ingest is run by your AI tool
of choice (Claude Code, Cursor, Codex, claude.ai routine). This command
prints (or writes) the template so you can hand it to your agent runner.

v0.2 adds direct ingest execution.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			path, ok := supportedIngestSources[source]
			if !ok {
				return fmt.Errorf("unknown source %q (supported: git, slack)", source)
			}
			body, err := ingestTemplates.ReadFile(path)
			if err != nil {
				return err
			}
			if dryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(body))
				return nil
			}
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			out := filepath.Join(cfg.WikiRoot, "agents", source+"-ingest.md")
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			if _, err := os.Stat(out); err == nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"%s already exists — edit in place or pass --dry-run to inspect the bundled template\n",
					out)
				return nil
			}
			if err := os.WriteFile(out, body, 0o644); err != nil {
				return err
			}
			rel, _ := filepath.Rel(cfg.WikiRoot, out)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"wrote %s — hand it to your agent runner (Claude Code / claude.ai routine / etc.)\n", rel)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the template instead of writing it")
	return cmd
}
