// Package cli wires up keeba's Cobra command tree.
package cli

import (
	"github.com/spf13/cobra"
)

// Version is the CLI's user-facing version string.
const Version = "v0.3.0-alpha"

// NewRoot returns a freshly-built root command. Each call yields an
// independent tree, which keeps test cases isolated.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "keeba",
		Short:         "Bootstrap an AI-native wiki.",
		Long:          "keeba — schema discipline, drift detection, MCP integration, ingest agents.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("keeba " + Version + "\n")

	root.PersistentFlags().String("wiki-root", "", "override wiki root (defaults to walking up for keeba.config.yaml). For `mcp serve`, pass `auto` to resolve at startup from cwd by walking up for .keeba/symbols.json — install --tool claude-code defaults to this so the MCP server follows whichever indexed repo Claude Code is launched in.")

	root.AddCommand(newLintCmd())
	root.AddCommand(newDriftCmd())
	root.AddCommand(newMetaCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newCompileCmd())
	root.AddCommand(newContextCmd())
	root.AddCommand(newHookCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newIndexCmd())
	root.AddCommand(newIngestCmd())
	root.AddCommand(newBenchCmd())
	root.AddCommand(newMCPCmd())

	return root
}
