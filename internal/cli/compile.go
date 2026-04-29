package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/aomerk/keeba/internal/symbol"
)

func newCompileCmd() *cobra.Command {
	var writeDir string
	cmd := &cobra.Command{
		Use:   "compile [repo-path]",
		Short: "Compile a repo's symbol graph into .keeba/symbols.json.",
		Long: `Compile walks the repo, extracts every function / method / class /
type / interface / exported var via Go AST (for .go files) and pattern-
based parsing (for Python / JS / TS / Rust / Java / Kotlin / Ruby / C /
C++), and writes the result to .keeba/symbols.json.

The output is consumed by ` + "`keeba mcp serve`" + ` to answer
"where is X defined / how is it used" queries from Claude Code,
Cursor, Codex, etc. without grep loops.

Pure Go, no CGO, no runtime deps.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoPath := "."
			if len(args) == 1 {
				repoPath = args[0]
			}
			target := writeDir
			if target == "" {
				target = repoPath
			}

			t0 := time.Now()
			idx, err := symbol.Compile(repoPath, target)
			if err != nil {
				return fmt.Errorf("compile %s: %w", repoPath, err)
			}
			elapsed := time.Since(t0)

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "keeba: compiled %d symbols across %d files in %s\n",
				idx.NumSymbols, idx.NumFiles, elapsed.Round(time.Millisecond))
			_, _ = fmt.Fprintf(out, "keeba: index at %s\n", symbol.IndexPath(target))

			byLang := idx.CountByLanguage()
			if len(byLang) > 0 {
				langs := make([]string, 0, len(byLang))
				for l := range byLang {
					langs = append(langs, l)
				}
				sort.Slice(langs, func(i, j int) bool { return byLang[langs[i]] > byLang[langs[j]] })
				_, _ = fmt.Fprintln(out, "keeba: by language:")
				for _, l := range langs {
					_, _ = fmt.Fprintf(out, "  %-5s %d\n", l, byLang[l])
				}
			}
			byKind := idx.CountByKind()
			if len(byKind) > 0 {
				kinds := make([]string, 0, len(byKind))
				for k := range byKind {
					kinds = append(kinds, k)
				}
				sort.Slice(kinds, func(i, j int) bool { return byKind[kinds[i]] > byKind[kinds[j]] })
				_, _ = fmt.Fprintln(out, "keeba: by kind:")
				for _, k := range kinds {
					_, _ = fmt.Fprintf(out, "  %-10s %d\n", k, byKind[k])
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&writeDir, "write-to", "",
		"directory to write .keeba/symbols.json into (default: the repo path).")
	return cmd
}
