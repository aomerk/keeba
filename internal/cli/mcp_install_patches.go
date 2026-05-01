package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// keebaMCPTools is the canonical set of tools `keeba mcp serve` exposes.
// Patched into agent allowed-tools lists by --patch-agents so user-defined
// sub-agents (Anthropic's general-purpose Task agent isn't editable, but
// these are) can actually invoke keeba. Without this patch, agents fall
// back to Read/Grep against full files even when keeba is registered —
// the hidden bug that defeats the savings pitch.
var keebaMCPTools = []string{
	"mcp__keeba__find_def",
	"mcp__keeba__search_symbols",
	"mcp__keeba__grep_symbols",
	"mcp__keeba__find_callers",
	"mcp__keeba__tests_for",
	"mcp__keeba__summary",
	"mcp__keeba__read_chunk",
	"mcp__keeba__session_stats",
	"mcp__keeba__query_documentation",
}

// allowedToolsBlockRE matches the `allowed-tools:` YAML key plus all of
// its bullet items. Frontmatter is always `---\n<yaml>\n---\n` and the
// list shape is consistent (`  - Name` per line) — no need for a full
// YAML parser, regex hits the structure exactly.
var allowedToolsBlockRE = regexp.MustCompile(`(?m)^allowed-tools:\n(?:  - [^\n]+\n)+`)

// keebaCLAUDEMDStart and keebaCLAUDEMDEnd bracket the keeba section in
// the user's global CLAUDE.md so re-running --with-claude-md is
// idempotent: we replace the bracketed range, never duplicate.
const (
	keebaCLAUDEMDStart = "<!-- keeba-managed:start -->"
	keebaCLAUDEMDEnd   = "<!-- keeba-managed:end -->"
)

// keebaCLAUDEMDSection is the guidance we inject. Tells main session to
// prefer keeba tools and NOT dispatch code-lookup investigations to
// sub-agents (general-purpose Task agent has no MCP access — falls back
// to Read/Grep, defeats the pitch).
const keebaCLAUDEMDSection = `
## Code investigation in keeba-indexed repos

When the active repo has ` + "`.keeba/symbols.json`" + ` AND keeba MCP server is loaded
(check ` + "`mcp__keeba__*`" + ` tools in /mcp):

- DO use keeba tools directly in main session: ` + "`find_def`" + `,
  ` + "`search_symbols`" + `, ` + "`grep_symbols`" + ` (literal=true for fixed
  strings), ` + "`find_callers`" + `, ` + "`tests_for`" + `, ` + "`summary`" + `,
  ` + "`read_chunk`" + `.
- Do NOT dispatch the investigation via Agent() / Task() — sub-agents
  (general-purpose especially) lack MCP access and will fall back to
  Read/Grep against full files, defeating keeba's purpose.
- Use Read only after find_def/grep_symbols pinpoints exact lines, then
  read_chunk for the body — never Read whole files unless explicitly asked.
`

// patchAgentFile injects the keeba MCP tools into one agent .md file's
// allowed-tools list. Idempotent: returns (false, nil) if the file
// already references mcp__keeba__find_def. Returns (false, nil) for
// files without an allowed-tools block — they aren't agents we can
// safely modify.
func patchAgentFile(path string) (bool, error) {
	body, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return false, err
	}
	src := string(body)

	// Idempotency check — any keeba MCP tool already present means we've
	// patched before. Re-running the install is safe.
	if strings.Contains(src, "mcp__keeba__find_def") {
		return false, nil
	}

	loc := allowedToolsBlockRE.FindStringIndex(src)
	if loc == nil {
		// Agent has no allowed-tools block, or uses a Bash(...) inline
		// shape we don't recognize. Skip — better than malformed YAML.
		return false, nil
	}

	addition := strings.Builder{}
	for _, t := range keebaMCPTools {
		addition.WriteString("  - ")
		addition.WriteString(t)
		addition.WriteString("\n")
	}
	patched := src[:loc[1]] + addition.String() + src[loc[1]:]

	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// patchAgentsDir walks dir for *.md agent definitions and patches each
// one. Returns the list of files actually modified (skips already-patched
// + non-agent files). Errors stop the walk so partial patching doesn't
// leave the user wondering which files got touched.
func patchAgentsDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var changed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		ok, err := patchAgentFile(full)
		if err != nil {
			return changed, fmt.Errorf("patch %s: %w", e.Name(), err)
		}
		if ok {
			changed = append(changed, e.Name())
		}
	}
	return changed, nil
}

// appendKeebaClaudeMD adds (or replaces) the keeba section in path. The
// section is bracketed by sentinel comments so re-running the install is
// idempotent — we always operate on the bracketed range, never duplicate.
// Returns (false, nil) when the existing section is already byte-identical
// to the canonical one, so the user knows nothing changed.
func appendKeebaClaudeMD(path string) (bool, error) {
	body, err := os.ReadFile(path) //nolint:gosec
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	src := string(body)
	canonical := keebaCLAUDEMDStart + "\n" + keebaCLAUDEMDSection + keebaCLAUDEMDEnd + "\n"

	startIdx := strings.Index(src, keebaCLAUDEMDStart)
	endIdx := strings.Index(src, keebaCLAUDEMDEnd)
	if startIdx >= 0 && endIdx > startIdx {
		// Replace existing section between sentinels (inclusive of end
		// marker + trailing newline if present).
		end := endIdx + len(keebaCLAUDEMDEnd)
		if end < len(src) && src[end] == '\n' {
			end++
		}
		existing := src[startIdx:end]
		if existing == canonical {
			return false, nil
		}
		patched := src[:startIdx] + canonical + src[end:]
		return true, os.WriteFile(path, []byte(patched), 0o644)
	}

	// Append fresh — ensure exactly one blank line before our section.
	prefix := src
	if !strings.HasSuffix(prefix, "\n\n") {
		if strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		} else if prefix != "" {
			prefix += "\n\n"
		}
	}
	return true, os.WriteFile(path, []byte(prefix+canonical), 0o644)
}
