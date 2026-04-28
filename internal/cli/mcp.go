package cli

import (
	"os"

	"github.com/spf13/cobra"

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
			cfg, err := loadCfg(cmd)
			if err != nil {
				return err
			}
			srv, err := mcp.New(cfg)
			if err != nil {
				return err
			}
			return srv.Serve(cmd.Context(), os.Stdin, os.Stdout)
		},
	})
	cmd.AddCommand(newMCPInstallCmd())
	return cmd
}
