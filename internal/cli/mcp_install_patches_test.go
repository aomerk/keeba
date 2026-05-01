package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchAgentFile_AddsKeebaTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "indexer-debug.md")
	body := `---
name: indexer-debug
description: x
allowed-tools:
  - Read
  - Grep
  - Bash
---

You are an agent.
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := patchAgentFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected change=true on first patch")
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "mcp__keeba__find_def") {
		t.Errorf("missing mcp__keeba__find_def after patch:\n%s", string(got))
	}
	if !strings.Contains(string(got), "mcp__keeba__session_stats") {
		t.Errorf("missing mcp__keeba__session_stats after patch:\n%s", string(got))
	}
	// Original tools must still be present.
	for _, want := range []string{"  - Read", "  - Grep", "  - Bash"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("dropped original tool %q:\n%s", want, string(got))
		}
	}
}

func TestPatchAgentFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	body := `---
name: x
allowed-tools:
  - Read
---
`
	_ = os.WriteFile(path, []byte(body), 0o644)
	if _, err := patchAgentFile(path); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	changed, err := patchAgentFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("second patch should be no-op (idempotent)")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("idempotent patch changed bytes:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestPatchAgentFile_SkipsNonAgent(t *testing.T) {
	// File without an allowed-tools block — patch must NOT corrupt it.
	dir := t.TempDir()
	path := filepath.Join(dir, "readme.md")
	body := "# Readme\n\nNo frontmatter here.\n"
	_ = os.WriteFile(path, []byte(body), 0o644)

	changed, err := patchAgentFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("non-agent file should not be patched")
	}
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Errorf("non-agent file mutated:\n%s", got)
	}
}

func TestPatchAgentsDir_PatchesEvery(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		body := `---
name: ` + name + `
allowed-tools:
  - Read
---
`
		_ = os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644)
	}
	changed, err := patchAgentsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 3 {
		t.Errorf("want 3 patched files, got %d (%v)", len(changed), changed)
	}
}

func TestPatchAgentsDir_MissingDirNotError(t *testing.T) {
	changed, err := patchAgentsDir(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Errorf("missing dir should not error: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("missing dir should yield no changes, got %v", changed)
	}
}

func TestAppendKeebaClaudeMD_AppendsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	original := "# Existing\n\nSome user content.\n"
	_ = os.WriteFile(path, []byte(original), 0o644)

	changed, err := appendKeebaClaudeMD(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected changed=true on first append")
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "## Code investigation in keeba-indexed repos") {
		t.Errorf("section heading missing:\n%s", got)
	}
	if !strings.HasPrefix(string(got), "# Existing") {
		t.Errorf("user content displaced — should remain at top:\n%s", got)
	}
	if !strings.Contains(string(got), keebaCLAUDEMDStart) || !strings.Contains(string(got), keebaCLAUDEMDEnd) {
		t.Errorf("sentinels missing — re-runs won't be idempotent")
	}
}

func TestAppendKeebaClaudeMD_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(path, []byte("user content\n"), 0o644)

	if _, err := appendKeebaClaudeMD(path); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	changed, err := appendKeebaClaudeMD(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("second append should be no-op")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("idempotent append changed bytes")
	}
}

func TestAppendKeebaClaudeMD_CreatesIfMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	// File doesn't exist — append should create it.
	changed, err := appendKeebaClaudeMD(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected changed=true when file missing")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(string(got), "## Code investigation in keeba-indexed repos") {
		t.Errorf("section missing in fresh file:\n%s", got)
	}
}
