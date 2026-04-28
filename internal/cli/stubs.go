package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// errStub is a typed error used by stub commands so the run-loop can
// translate it into exit code 2 without printing usage.
type errStub struct{ msg string }

func (e *errStub) Error() string { return e.msg }

// IsStubError reports whether err came from an unimplemented subcommand.
func IsStubError(err error) bool {
	_, ok := err.(*errStub)
	return ok
}

func stub(name string) *errStub {
	return &errStub{msg: fmt.Sprintf("keeba %s: not yet implemented", name)}
}

// silentExit is returned by commands that have already printed a summary and
// just want a non-zero exit code without main() echoing the error string.
type silentExit struct{}

func (silentExit) Error() string { return "silent exit" }

// IsSilentExit reports whether err is the silent-exit sentinel.
func IsSilentExit(err error) bool {
	_, ok := err.(silentExit)
	return ok
}

// errSilentFail is the shared instance returned by lint/drift/meta when they
// already printed their report.
var errSilentFail silentExit

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a fresh wiki repo (stub).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return stub("init")
		},
	}
}

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Semantic search over the wiki (stub).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return stub("search")
		},
	}
	cmd.Flags().IntP("top-k", "k", 5, "number of results to return")
	return cmd
}

func newIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest SOURCE",
		Short: "Run an ingest agent (stub).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return stub("ingest")
		},
	}
	cmd.Flags().Bool("dry-run", false, "preview ingest output without writing")
	return cmd
}

func newBenchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Benchmark wiki vs raw sources (stub).",
		RunE: func(_ *cobra.Command, _ []string) error {
			return stub("bench")
		},
	}
	cmd.Flags().String("diff", "", "compare to a previous bench JSON")
	return cmd
}

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server commands (stub).",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Run the keeba MCP server over stdio (stub).",
		RunE: func(_ *cobra.Command, _ []string) error {
			return stub("mcp serve")
		},
	})
	return cmd
}
