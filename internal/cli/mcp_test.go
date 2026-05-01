package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// fakeServeCmd builds a minimal cobra.Command whose flag set mirrors
// the persistent --wiki-root flag the real serve command inherits from
// root. Tests use it to drive resolveAutoWikiRoot in isolation.
func fakeServeCmd(initial string) *cobra.Command {
	c := &cobra.Command{Use: "serve"}
	var v string
	c.Flags().StringVar(&v, "wiki-root", initial, "test")
	if initial != "" {
		_ = c.Flags().Set("wiki-root", initial)
	}
	return c
}

// chdir helper — restores the original cwd at test cleanup so parallel
// tests don't poison each other.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestResolveAutoWikiRoot_PrefersSymbolsJSONOverConfigYAML(t *testing.T) {
	// Both signals present at the same root — symbols.json wins
	// because that's what the MCP server actually consumes.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".keeba"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".keeba", "symbols.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keeba.config.yaml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	cmd := fakeServeCmd("auto")
	if err := resolveAutoWikiRoot(cmd); err != nil {
		t.Fatalf("resolveAutoWikiRoot: %v", err)
	}
	got := cmd.Flags().Lookup("wiki-root").Value.String()
	want, _ := filepath.EvalSymlinks(root)
	gotEval, _ := filepath.EvalSymlinks(got)
	if gotEval != want {
		t.Errorf("--wiki-root after resolve = %q, want %q", got, root)
	}
}

func TestResolveAutoWikiRoot_FallsBackToConfigYAMLWhenNoSymbols(t *testing.T) {
	// Wiki-only repo (keeba.config.yaml but no compiled symbol graph) —
	// keep the existing keeba.config.yaml fallback so `keeba mcp serve`
	// still comes up. Tools will return the "run keeba compile" hint
	// when called.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keeba.config.yaml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	cmd := fakeServeCmd("auto")
	if err := resolveAutoWikiRoot(cmd); err != nil {
		t.Fatal(err)
	}
	got := cmd.Flags().Lookup("wiki-root").Value.String()
	want, _ := filepath.EvalSymlinks(root)
	gotEval, _ := filepath.EvalSymlinks(got)
	if gotEval != want {
		t.Errorf("--wiki-root after resolve = %q, want %q (config.yaml fallback)", got, root)
	}
}

func TestResolveAutoWikiRoot_FallsBackToCwdWhenNothingFound(t *testing.T) {
	// No signals anywhere walking up from cwd. Server still boots so
	// the user gets the "no symbol graph" hint when they call tools,
	// rather than the install failing outright.
	root := t.TempDir()
	chdir(t, root)
	cmd := fakeServeCmd("auto")
	if err := resolveAutoWikiRoot(cmd); err != nil {
		t.Fatal(err)
	}
	got := cmd.Flags().Lookup("wiki-root").Value.String()
	cwd, _ := os.Getwd()
	wantEval, _ := filepath.EvalSymlinks(cwd)
	gotEval, _ := filepath.EvalSymlinks(got)
	if gotEval != wantEval {
		t.Errorf("--wiki-root after resolve = %q, want cwd %q", got, cwd)
	}
}

func TestResolveAutoWikiRoot_EmptyTreatedAsAuto(t *testing.T) {
	// Older configs may pass --wiki-root="" (empty). Treat the same
	// as `auto` for back-compat.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".keeba"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".keeba", "symbols.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)
	cmd := fakeServeCmd("")
	if err := resolveAutoWikiRoot(cmd); err != nil {
		t.Fatal(err)
	}
	got := cmd.Flags().Lookup("wiki-root").Value.String()
	if got == "" || got == "auto" {
		t.Errorf("empty --wiki-root not resolved: got %q", got)
	}
}

func TestResolveAutoWikiRoot_ExplicitPathPassthrough(t *testing.T) {
	// User pinned an explicit path with --wiki-root-override at install
	// time. resolveAutoWikiRoot must NOT touch it — pinning is a
	// deliberate opt-out of auto-resolution.
	want := "/some/explicit/path"
	cmd := fakeServeCmd(want)
	if err := resolveAutoWikiRoot(cmd); err != nil {
		t.Fatal(err)
	}
	got := cmd.Flags().Lookup("wiki-root").Value.String()
	if got != want {
		t.Errorf("explicit path mutated: got %q, want %q", got, want)
	}
}
