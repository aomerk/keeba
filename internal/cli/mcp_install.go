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
		tool     string
		scope    string
		wikiRoot string
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
				return installClaudeCode(cmd, abs, scope)
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
	_ = cmd.MarkFlagRequired("tool")
	return cmd
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
	bin, err := os.Executable()
	if err != nil {
		bin = "keeba"
	}
	entry := fmt.Sprintf(`
[mcp_servers.keeba]
command = %q
args = ["mcp", "serve", "--wiki-root", %q]
`, bin, wikiRoot)

	if existing, err := os.ReadFile(target); err == nil { //nolint:gosec
		if !containsKeebaCodex(string(existing)) {
			merged := string(existing)
			if !endsWithNewline(merged) {
				merged += "\n"
			}
			merged += entry
			if err := os.WriteFile(target, []byte(merged), 0o644); err != nil {
				return err
			}
		}
	} else if os.IsNotExist(err) {
		if err := os.WriteFile(target, []byte(entry), 0o644); err != nil {
			return err
		}
	} else {
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
