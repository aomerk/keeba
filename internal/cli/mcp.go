package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server commands.",
	}
	var codec string
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the keeba MCP server over stdio.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			srv, err := mcp.New(cfg)
			if err != nil {
				return err
			}
			srv.Version = Version
			if codec != "" {
				srv.Codec = codec
			}

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
	}
	serveCmd.Flags().StringVar(&codec, "codec", "",
		"response codec: `full` (default; full Symbol per row) or `lean` (interned codes + minimal metadata; agent calls `expand` for sig/doc on demand). Phase 15A: lean mode applies to find_def only — other tools fall through to full until the codec proves itself in real /cost A/Bs.")
	cmd.AddCommand(serveCmd)
	cmd.AddCommand(newMCPInstallCmd())
	return cmd
}
