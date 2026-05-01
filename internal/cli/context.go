package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/context"
)

// newContextCmd is the day-1 demo CLI: takes a natural-language prompt,
// runs the symbol-graph queries an agent would have run, prints a
// paste-ready markdown block (or JSON) to stdout. Bypasses the MCP
// integration cliff — works in any AI tool that accepts pasted context,
// no per-tool config needed.
func newContextCmd() *cobra.Command {
	var (
		repoPath string
		jsonOut  bool
		maxBytes int
	)
	cmd := &cobra.Command{
		Use:   "context [prompt...]",
		Short: "Pre-ground a prompt with symbol-graph hits — paste-ready context block.",
		Long: `Take a natural-language prompt, run the symbol-graph queries an
agent would have run (find_def per identifier, BM25 over the prompt as
free text, grep_symbols literal=true per quoted string), and print a
markdown block to stdout. Paste the block + your follow-up question to
any AI tool — no MCP integration required.

Examples:
  keeba context "investigate why MonSetPromote admits sub-threshold addresses"
  keeba context --json "find_def for AuthMiddleware and its callers"
  keeba context --max-bytes 2000 "stripe webhook secret handling"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			if strings.TrimSpace(prompt) == "" {
				return fmt.Errorf("prompt is required")
			}
			rep, err := context.Build(repoPath, prompt, context.Options{MaxBytes: maxBytes})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				b, err := json.MarshalIndent(rep, "", "  ")
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(out, string(b))
				return nil
			}
			_, _ = fmt.Fprint(out, context.RenderMarkdown(rep))
			return nil
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", ".",
		"path to the keeba-compiled repo (must contain .keeba/symbols.json)")
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"emit JSON instead of markdown (for scripted consumers)")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", 0,
		"cap the rendered markdown size (default 0 = no cap). Useful for piping into a tool with a limited context window.")
	return cmd
}
