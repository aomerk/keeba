package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// keebaCLAUDEMDSection is the guidance we inject into the user's global
// ~/.claude/CLAUDE.md. The earlier gentle wording ("DO use keeba …") wasn't
// enough — real-world A/B showed Claude still defaulted to Read/Grep
// without an explicit prompt nudge. This version is assertive: imperative
// voice, BEFORE Read/Grep, and a token-cost framing so the agent has its
// own reason to choose keeba (verified ~30% session savings on real
// codebases). Headed by a sentinel range so re-runs are idempotent.
const keebaCLAUDEMDSection = `
## Code investigation in keeba-indexed repos

When the active repo contains ` + "`.keeba/symbols.json`" + ` AND keeba MCP tools
(` + "`mcp__keeba__*`" + ` in ` + "`/mcp`" + `) are loaded, **the keeba MCP tools are the
canonical way to investigate code**. Using ` + "`Read`" + ` or ` + "`Grep`" + ` instead burns
~30% more tokens (cache_read) for the same answer — measured on a real
Go codebase, both arms produced identical findings at identical
file:line citations. The only thing that changed was cost.

Routing rules (apply in main session, before any other action):

- For "where is X defined?" or "what is X?" → call
  ` + "`mcp__keeba__find_def(name=\"X\")`" + ` BEFORE Read.
- For "what calls X?" / "what would break if I rename X?" → call
  ` + "`mcp__keeba__find_callers(name=\"X\")`" + ` BEFORE grep. Pair with
  ` + "`mcp__keeba__find_refs`" + ` if available (covers type-position usage that
  ` + "`find_callers`" + ` doesn't).
- For "what tests cover X?" → call ` + "`mcp__keeba__tests_for(name=\"X\")`" + `.
- For free-text concept ("auth handler", "stripe webhook") → call
  ` + "`mcp__keeba__search_symbols(query=\"...\")`" + `. BM25 over name+sig+doc.
- For magic strings, env-var keys, regex patterns inside function bodies
  → call ` + "`mcp__keeba__grep_symbols(pattern=\"...\", literal=true)`" + ` BEFORE
  ` + "`Bash(rg ...)`" + ` or full-file Read.
- For "show me the body of X" once you have file:line → call
  ` + "`mcp__keeba__read_chunk(file=\"...\", start_line=N, end_line=M)`" + ` —
  never read the whole file.
- For per-file overview → ` + "`mcp__keeba__summary(file=\"path/prefix/\")`" + `.
- After a multi-tool investigation, optionally call
  ` + "`mcp__keeba__session_stats`" + ` to print the token-savings receipt
  (` + "`bytes_returned`" + ` vs ` + "`bytes_alternative`" + `).

Hard rules:

- **NEVER dispatch a code-lookup investigation via ` + "`Agent()`" + ` or ` + "`Task()`" + `**
  unless explicitly told to. Sub-agents (especially the built-in
  general-purpose one) do NOT inherit user-scope MCP servers — they fall
  back to ` + "`Read`" + ` and ` + "`Bash(rg)`" + ` against full files, undoing keeba's
  savings. Run lookups in main session and synthesize the answer there.
- **NEVER ` + "`Read`" + ` whole files when keeba can pinpoint a range.** ` + "`find_def`" + `
  / ` + "`grep_symbols`" + ` give you ` + "`file:start_line-end_line`" + ` for free. Pair
  with ` + "`read_chunk`" + ` for the body, never with bare ` + "`Read(path)`" + `.
- **NEVER ` + "`Bash(rg ...)`" + ` for a term that ` + "`grep_symbols`" + ` can find.** The
  regex sweep over symbol bodies is the same operation, but the response
  is bounded by symbol limits and gets ` + "`bytes_alternative`" + ` accounting.

Answer discipline (output-token savings):

Output tokens are priced ~50× cache_read tokens. Codec layers shrink
cache_read but not output, which is why session savings ceiling at ~30%
on cache_read alone. The remaining wins come from cutting output bloat.

- **ONE ` + "`find_def`" + ` hit → that's the answer. STOP.** Do not also call
  ` + "`find_callers`" + `, ` + "`tests_for`" + `, etc. unless the user asked. Single hits
  are the answer; extra calls are wasted output (each tool roundtrip
  adds preamble + restatement tokens to the next assistant turn).
- **Quote tool-result rows verbatim.** Paste ` + "`file:line`" + ` plus the snippet
  unchanged. Do NOT paraphrase ("the function HandleAuth lives at...") —
  restatement is the single biggest output-token sink in code
  investigations. The agent already has the data; the user does too.
- **NO preamble.** Skip "I'll investigate by...", "Let me start by...",
  "First, I'll call...". The user reads the tool call; narrating it
  again is wasted tokens.
- **NO closing summary.** Skip "To summarize...", "Based on my
  investigation...", "In short...". The cited file:line IS the summary.
- **Conclusion first.** Lead with the answer. Evidence cites follow.
  Multi-step reasoning stays in your head — write the conclusion, not
  the chain-of-thought, unless the user asks for the derivation.

Fallback: if a keeba tool returns ` + "`\"no symbol graph in this directory — run keeba compile first\"`" + `,
mention it once to the user (a one-time suggestion to run ` + "`keeba compile`" + `)
and proceed with ` + "`Read`" + ` / ` + "`Grep`" + `. Don't loop on the hint.
`

// keebaHookSentinel is the marker substring we embed in the hook
// command so re-running the install is idempotent — settings.json may
// have other UserPromptSubmit entries, we just need to recognize ours.
// Anyone else writing a UserPromptSubmit hook with this marker is asking
// for it.
const keebaHookSentinel = "keeba hook user-prompt-submit"

// installUserPromptSubmitHook upserts a Claude Code UserPromptSubmit
// hook that invokes `<keebaBin> hook user-prompt-submit` on every prompt.
// Idempotent — re-runs replace the existing entry rather than duplicate.
// Returns (changed, err): changed=false means the existing entry was
// already byte-identical, no write needed.
func installUserPromptSubmitHook(settingsPath, keebaBin string) (bool, error) {
	body, err := os.ReadFile(settingsPath) //nolint:gosec
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	settings := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &settings); err != nil {
			return false, fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	command := keebaBin + " hook user-prompt-submit"
	desired := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
				"timeout": 5000,
			},
		},
	}

	// Filter existing UserPromptSubmit entries: drop any that already
	// contain the sentinel (our prior install) and keep the rest.
	existing, _ := hooks["UserPromptSubmit"].([]any)
	pruned := make([]any, 0, len(existing)+1)
	for _, entry := range existing {
		if !entryReferencesKeeba(entry) {
			pruned = append(pruned, entry)
		}
	}
	pruned = append(pruned, desired)

	hooks["UserPromptSubmit"] = pruned
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')
	if string(out) == string(body) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// entryReferencesKeeba walks one UserPromptSubmit entry's nested
// "hooks" array and returns true when any command contains the keeba
// sentinel. Used by installUserPromptSubmitHook to dedupe across runs.
func entryReferencesKeeba(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	inner, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hm["command"].(string)
		if strings.Contains(cmd, keebaHookSentinel) {
			return true
		}
	}
	return false
}

// looksLikeWorktree returns true when path appears to be inside a git
// worktree rather than a primary checkout. We check two cheap signals:
//
//  1. Any ancestor directory is named ` + "`worktrees`" + ` and contains a sibling
//     ` + "`.git/worktrees`" + ` (Claude Code's own ` + "`.claude/worktrees/<name>/`" + `
//     convention, plus plain ` + "`git worktree add`" + ` outputs which sit under
//     ` + "`<repo>/.git/worktrees/<name>`" + `).
//  2. The path's ` + "`.git`" + ` is a regular file (worktree pointer) rather than
//     a directory.
//
// Fires the warning at install time so users running ` + "`keeba mcp install`" + `
// from a worktree see the wiki-root mismatch problem before their first
// failed Claude Code session, not after.
func looksLikeWorktree(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// Signal 1: path component named "worktrees" anywhere above us.
	parts := strings.Split(filepath.ToSlash(abs), "/")
	if slices.Contains(parts, "worktrees") {
		return true
	}
	// Signal 2: .git is a regular file (worktree pointer).
	if info, err := os.Lstat(filepath.Join(abs, ".git")); err == nil && !info.IsDir() {
		return true
	}
	return false
}

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
