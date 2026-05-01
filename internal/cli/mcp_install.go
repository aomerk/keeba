package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// supportedTools are the MCP-host CLIs `keeba mcp install` knows how to wire
// itself into. Adding a new entry is one Installer func + a switch arm.
var supportedTools = map[string]string{
	"claude-code": "Anthropic Claude Code (claude CLI)",
	"cursor":      "Cursor IDE (.cursor/mcp.json)",
	"codex":       "OpenAI Codex CLI (~/.codex/config.toml)",
}

func newMCPInstallCmd() *cobra.Command {
	var (
		tool            string
		scope           string
		wikiRoot        string
		patchAgents     bool
		withClaudeMD    bool
		withHook        bool
		withOutputStyle bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Wire keeba's MCP server into Claude Code, Cursor, or Codex.",
		Long: `Adds (or upserts) the keeba MCP server entry in the chosen tool's config.

Idempotent — safe to re-run.

Examples:
  keeba mcp install --tool claude-code
  keeba mcp install --tool cursor --scope project
  keeba mcp install --tool codex
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, ok := supportedTools[tool]; !ok {
				keys := make([]string, 0, len(supportedTools))
				for k := range supportedTools {
					keys = append(keys, k)
				}
				return fmt.Errorf("--tool must be one of %v; got %q", keys, tool)
			}
			if wikiRoot == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				wikiRoot = cwd
			}
			abs, err := filepath.Abs(wikiRoot)
			if err != nil {
				return err
			}
			switch tool {
			case "claude-code":
				if looksLikeWorktree(abs) {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(),
						"WARNING: %q looks like a git worktree. keeba MCP serves symbols from the path you set with --wiki-root-override (default: cwd). If the worktree's branch is at a different commit than the main checkout, keeba will see stale symbols and Claude Code will silently fall back to Read/Grep. Either run `keeba compile` per-worktree, or accept that this MCP install serves the worktree's snapshot only.\n\n",
						abs)
				}
				if err := installClaudeCode(cmd, abs, scope); err != nil {
					return err
				}
				return applyClaudeCodePatches(cmd, patchAgents, withClaudeMD, withHook, withOutputStyle)
			case "cursor":
				return installCursor(cmd, abs, scope)
			case "codex":
				return installCodex(cmd, abs)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "", "target tool: claude-code | cursor | codex")
	cmd.Flags().StringVar(&scope, "scope", "user", "scope: user (default) | project")
	cmd.Flags().StringVar(&wikiRoot, "wiki-root-override", "", "wiki root the MCP server should serve (default: cwd)")
	cmd.Flags().BoolVar(&patchAgents, "patch-agents", false,
		"claude-code only: add mcp__keeba__* to the allowed-tools list of every ~/.claude/agents/*.md so user-defined sub-agents can invoke keeba (Anthropic's built-in general-purpose agent isn't user-editable; combine with --with-claude-md to steer main session away from dispatching).")
	cmd.Flags().BoolVar(&withClaudeMD, "with-claude-md", false,
		"claude-code only: append (or update) a keeba section in ~/.claude/CLAUDE.md telling main session to use keeba tools directly and NOT dispatch code-lookup investigations to sub-agents (which lack MCP access).")
	cmd.Flags().BoolVar(&withHook, "with-hook", false,
		"claude-code only: register a UserPromptSubmit hook that runs `keeba context` on every prompt and injects the symbol-graph evidence as additionalContext. Invisible to the user — agent sees the file:line grounding before it picks any tool. Closes the prompt-nudge gap that --patch-agents + --with-claude-md leave open.")
	cmd.Flags().BoolVar(&withOutputStyle, "with-output-style", false,
		"claude-code only: install ~/.claude/output-styles/keeba.md — a terse engineering output style that suppresses preamble, restatement of tool results, and closing summaries (the three biggest output-token sinks). Output tokens are priced ~50× cache_read so cutting them moves the dollar needle past the codec ceiling. Activate per-session with /output-style keeba.")
	_ = cmd.MarkFlagRequired("tool")
	return cmd
}

// applyClaudeCodePatches applies the optional --patch-agents,
// --with-claude-md, --with-hook, and --with-output-style fixes after
// the MCP server registration succeeds. Each patch is idempotent —
// re-running prints "no change" instead of duplicating. Failures are
// surfaced but don't roll back the MCP registration.
func applyClaudeCodePatches(cmd *cobra.Command, patchAgents, withClaudeMD, withHook, withOutputStyle bool) error {
	if !patchAgents && !withClaudeMD && !withHook && !withOutputStyle {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(),
			"tip: sub-agents (general-purpose Task) lack MCP access by default. Re-run with --patch-agents --with-claude-md --with-hook --with-output-style to make Claude Code actually use keeba AND suppress the output-token bloat (preamble + restatement + summaries) that ceilings session savings at ~30%. Output style activates per-session with /output-style keeba.")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if patchAgents {
		dir := filepath.Join(home, ".claude", "agents")
		changed, err := patchAgentsDir(dir)
		if err != nil {
			return fmt.Errorf("patch agents: %w", err)
		}
		if len(changed) == 0 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"agent files in %s — already patched (no change)\n", dir)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"patched %d agent file(s) in %s with mcp__keeba__* allowed-tools: %v\n",
				len(changed), dir, changed)
		}
	}
	if withClaudeMD {
		path := filepath.Join(home, ".claude", "CLAUDE.md")
		changed, err := appendKeebaClaudeMD(path)
		if err != nil {
			return fmt.Errorf("append CLAUDE.md: %w", err)
		}
		if changed {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"updated %s with keeba code-investigation guidance\n", path)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"%s already has the keeba section (no change)\n", path)
		}
	}
	if withHook {
		path := filepath.Join(home, ".claude", "settings.json")
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate keeba binary: %w", err)
		}
		// Prefer the package-managed `keeba` on PATH for portability —
		// users update via go install / brew and the hook should pick
		// up the new binary automatically. Fall back to the absolute
		// path of the running executable.
		bin := exe
		if look, err := exec.LookPath("keeba"); err == nil {
			bin = look
		}
		changed, err := installUserPromptSubmitHook(path, bin)
		if err != nil {
			return fmt.Errorf("install hook: %w", err)
		}
		if changed {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"registered UserPromptSubmit hook in %s — Claude Code will pre-ground every prompt with keeba context (restart Claude Code to pick it up)\n", path)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"%s already has the keeba hook (no change)\n", path)
		}
	}
	if withOutputStyle {
		path := filepath.Join(home, ".claude", "output-styles", "keeba.md")
		changed, err := installKeebaOutputStyle(path)
		if err != nil {
			return fmt.Errorf("install output style: %w", err)
		}
		if changed {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"installed keeba output style at %s — activate per-session with `/output-style keeba` (suppresses preamble + tool-result restatement + closing summaries; output tokens drop, dollar cost drops with them)\n", path)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"%s already has the keeba output style (no change)\n", path)
		}
	}
	return nil
}

func installClaudeCode(cmd *cobra.Command, wikiRoot, scope string) error {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("`claude` CLI not on PATH; install Claude Code first: %w", err)
	}
	scopeFlag := "user"
	if scope == "project" {
		scopeFlag = "project"
	}
	// Idempotency: if keeba is already in this scope's config, no-op. The
	// `claude mcp` CLI errors on duplicate adds, so we check first.
	if claudeMCPHasKeeba(bin, scopeFlag) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"keeba MCP already installed in Claude Code (%s scope) — no change\n", scopeFlag)
		return nil
	}
	args := []string{
		"mcp", "add", "keeba",
		"--scope", scopeFlag,
		"--",
		"keeba", "mcp", "serve", "--wiki-root", wikiRoot,
	}
	out, err := exec.Command(bin, args...).CombinedOutput() //nolint:gosec
	if err != nil {
		return fmt.Errorf("claude mcp add: %w\n%s", err, string(out))
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"installed keeba MCP into Claude Code (%s scope) → serves %s\n", scopeFlag, wikiRoot)
	return nil
}

// claudeMCPHasKeeba returns true when `claude mcp list` already includes a
// server named "keeba" — covering both user-scope and project-scope configs.
// Conservatively returns false on any error so we still attempt the add.
func claudeMCPHasKeeba(bin, _ string) bool {
	out, err := exec.Command(bin, "mcp", "list").Output() //nolint:gosec
	if err != nil {
		return false
	}
	for _, line := range stringSplit(string(out), "\n") {
		// `claude mcp list` prints `keeba: <command> [...]` per server.
		if line == "" {
			continue
		}
		// Match start-of-line "keeba" up to a colon or whitespace.
		if len(line) >= 6 && line[:5] == "keeba" && (line[5] == ':' || line[5] == ' ' || line[5] == '\t') {
			return true
		}
	}
	return false
}

type cursorConfig struct {
	MCPServers map[string]cursorServer `json:"mcpServers"`
}

type cursorServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func installCursor(cmd *cobra.Command, wikiRoot, scope string) error {
	bin, err := exec.LookPath("keeba")
	if err != nil {
		// Use the absolute path of this binary as a fallback. The user might
		// have the binary outside their PATH at install time.
		exe, ferr := os.Executable()
		if ferr != nil {
			return fmt.Errorf("locate keeba binary: %w / %w", err, ferr)
		}
		bin = exe
	}
	target, err := cursorTarget(wikiRoot, scope)
	if err != nil {
		return err
	}
	cfg := cursorConfig{MCPServers: map[string]cursorServer{}}
	if existing, err := os.ReadFile(target); err == nil { //nolint:gosec
		_ = json.Unmarshal(existing, &cfg)
		if cfg.MCPServers == nil {
			cfg.MCPServers = map[string]cursorServer{}
		}
	}
	cfg.MCPServers["keeba"] = cursorServer{
		Command: bin,
		Args:    []string{"mcp", "serve", "--wiki-root", wikiRoot},
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, append(body, '\n'), 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"installed keeba MCP into Cursor → %s\n", target)
	return nil
}

func cursorTarget(wikiRoot, scope string) (string, error) {
	if scope == "project" {
		return filepath.Join(wikiRoot, ".cursor", "mcp.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "mcp.json"), nil
}

func installCodex(cmd *cobra.Command, wikiRoot string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	target := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	// Prefer the keeba binary on PATH so the user's package-managed install
	// is what Codex launches; fall back to the currently-running executable.
	bin, lookErr := exec.LookPath("keeba")
	if lookErr != nil {
		exe, ferr := os.Executable()
		if ferr != nil {
			return fmt.Errorf("locate keeba binary: %w / %w", lookErr, ferr)
		}
		bin = exe
	}
	// Schema verified against openai/codex codex-rs/core/config.schema.json
	// (RawMcpServerConfig): command (string), args (array of strings).
	entry := fmt.Sprintf(`
[mcp_servers.keeba]
command = %q
args = ["mcp", "serve", "--wiki-root", %q]
`, bin, wikiRoot)

	switch existing, err := os.ReadFile(target); { //nolint:gosec
	case err == nil:
		if containsKeebaCodex(string(existing)) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"keeba MCP already in %s — no change\n", target)
			return nil
		}
		merged := string(existing)
		if !endsWithNewline(merged) {
			merged += "\n"
		}
		merged += entry
		if err := os.WriteFile(target, []byte(merged), 0o644); err != nil {
			return err
		}
	case os.IsNotExist(err):
		if err := os.WriteFile(target, []byte(entry), 0o644); err != nil {
			return err
		}
	default:
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"installed keeba MCP into Codex → %s\n", target)
	return nil
}

func containsKeebaCodex(toml string) bool {
	for _, line := range stringSplit(toml, "\n") {
		if line == "[mcp_servers.keeba]" {
			return true
		}
	}
	return false
}

func endsWithNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}

// stringSplit avoids importing strings just for one helper in this file.
func stringSplit(s, sep string) []string {
	var out []string
	start := 0
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			out = append(out, s[start:i])
			start = i + len(sep)
		}
	}
	out = append(out, s[start:])
	return out
}
