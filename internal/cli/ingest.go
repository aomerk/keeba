package cli

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/ingest"
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
	var (
		dryRun  bool
		execute bool
		repo    string
		since   string
	)
	cmd := &cobra.Command{
		Use:   "ingest SOURCE",
		Short: "Run an ingest agent or write its prompt template.",
		Long: `By default, drops the agent prompt template into agents/<source>-ingest.md
for an external runner (Claude Code, Cursor, Codex, claude.ai routine).

With --execute, runs the heuristic in-process instead. v0.3 ships --execute
for git only: it walks ` + "`git log`" + ` for the configured repos, classifies
commits by regex (BREAKING:, incident keywords, ADR markers, major dep
bumps), and writes/appends to log.md, investigations/, decisions/.

Examples:
  keeba ingest git --dry-run                # print the template
  keeba ingest git                          # write template to agents/
  keeba ingest git --execute --repo .       # actually digest the last 7 days
  keeba ingest git --execute --repo ../my-app --since 30.days.ago --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			if execute {
				if source != "git" {
					return fmt.Errorf("--execute is only wired for git in v0.3; got %q", source)
				}
				return runGitIngest(cmd, repo, since, dryRun)
			}

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
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing (works with --execute too)")
	cmd.Flags().BoolVar(&execute, "execute", false, "run the ingest heuristic in-process instead of writing the template")
	cmd.Flags().StringVar(&repo, "repo", ".", "path to the source repo (for --execute)")
	cmd.Flags().StringVar(&since, "since", "7.days.ago", "git --since spec for --execute")
	return cmd
}

func runGitIngest(cmd *cobra.Command, repo, since string, dryRun bool) error {
	cfg, err := loadCfg(cmd)
	if err != nil {
		return err
	}
	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	actions, err := ingest.Git(cfg.WikiRoot, repoAbs, since, dryRun)
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "no durable signal in %s since %s\n", repoAbs, since)
		return nil
	}
	verb := "wrote"
	if dryRun {
		verb = "would write"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %d action(s):\n", verb, len(actions))
	for _, a := range actions {
		switch {
		case a.AppendPath != "":
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  · append %-12s %s — %s\n",
				a.AppendPath, a.Class, a.Commit.SHA[:7])
		case a.TargetPath != "":
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  · create %-12s %s — %s\n",
				a.TargetPath, a.Class, a.Commit.SHA[:7])
		}
	}
	return nil
}
