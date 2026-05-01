package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server commands.",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Run the keeba MCP server over stdio.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resolveAutoWikiRoot(cmd); err != nil {
				return err
			}
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			srv, err := mcp.New(cfg)
			if err != nil {
				return err
			}
			srv.Version = Version

			// Start the live-symbol watcher in the background when a
			// graph is loaded. Edits to any indexed file re-extract that
			// file in <50ms, so find_def / read_chunk responses stay
			// accurate even while Claude Code (or the user's IDE) is
			// rewriting the source under the agent's feet.
			if li := srv.LiveIndex(); li != nil {
				go func() { _ = li.Run(cmd.Context()) }()
				defer func() { _ = li.Close() }()
			}

			err = srv.Serve(cmd.Context(), os.Stdin, os.Stdout)
			// Receipt — visible in the agent's MCP server log even if
			// the agent never calls session_stats explicitly. This is
			// the marketing artifact: every session ends with a one-line
			// "you saved N tokens with keeba" message in the user's
			// editor's logs.
			fmt.Fprintln(os.Stderr, srv.Stats().SummaryLine())
			return err
		},
	})
	cmd.AddCommand(newMCPInstallCmd())
	return cmd
}

// resolveAutoWikiRoot rewrites the `--wiki-root` flag in place when the
// caller passed `auto` or left it empty. The MCP server is launched by
// Claude Code (or Cursor / Codex) in the cwd of the editor session, so
// at startup we walk up from cwd looking for `.keeba/symbols.json`. The
// install command writes `--wiki-root auto` by default — that flips the
// MCP server from "always serve the cwd-at-install-time repo" to "serve
// whichever indexed repo the editor session is in". Cross-repo
// investigations now hit the right symbol graph instead of falling back
// to Read/Grep when the install-time root doesn't match the cwd.
//
// Resolution order:
//
//  1. `.keeba/symbols.json` walking up from cwd (canonical signal —
//     it's what the MCP server actually consumes).
//  2. `keeba.config.yaml` walking up from cwd (a wiki repo without a
//     compiled graph; lets `keeba mcp serve` still come up cleanly so
//     the user can `keeba compile` from there).
//  3. Cwd itself (no graph found anywhere; server still starts and
//     emits the "no symbol graph" hint when tools are called).
//
// Receipt to stderr at every startup so the user can see which root the
// session bound to — same channel as the end-of-session savings line.
func resolveAutoWikiRoot(cmd *cobra.Command) error {
	flag := cmd.Flags().Lookup("wiki-root")
	if flag == nil {
		return nil
	}
	value := flag.Value.String()
	if value != "" && value != "auto" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	resolved := cwd
	source := "cwd (no .keeba/symbols.json found walking up)"
	if hit := config.FindCodeGraphRoot(cwd); hit != "" {
		resolved = hit
		source = ".keeba/symbols.json"
	} else if hit := config.FindWikiRoot(cwd); hit != "" {
		resolved = hit
		source = "keeba.config.yaml"
	}
	if err := flag.Value.Set(resolved); err != nil {
		return fmt.Errorf("set --wiki-root: %w", err)
	}
	fmt.Fprintf(os.Stderr,
		"keeba mcp serve: auto-resolved --wiki-root to %s (via %s; cwd=%s)\n",
		resolved, source, cwd)
	return nil
}
