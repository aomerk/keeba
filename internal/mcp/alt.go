package mcp

import (
	"os"
)

// sumFileSizes returns the total byte size of the unique files in the
// given list. Files that fail to stat (missing, permission, etc.) are
// skipped — the receipt is best-effort, not strict. Path safety via
// safeJoin: the file list comes from the compiled symbol graph, so
// paths are repo-relative and safe by construction, but routing through
// safeJoin keeps the surface uniform with other readers and gives the
// computer a hard guard if a graph entry ever drifts to absolute paths.
//
// Used by every per-tool alt-computer (find_def / search_symbols /
// find_callers / tests_for / summary / grep_symbols) to power the
// "bytes_alternative" column of session_stats — the honest answer to
// "how many bytes would the agent have pulled in unfiltered to get
// this result without keeba?".
func sumFileSizes(root string, files []string) int {
	seen := map[string]struct{}{}
	total := 0
	for _, f := range files {
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		abs, err := safeJoin(root, f)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		total += int(info.Size())
	}
	return total
}
