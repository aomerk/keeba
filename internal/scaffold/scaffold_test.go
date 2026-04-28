package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldFreshDir(t *testing.T) {
	out := t.TempDir()
	if err := Scaffold(out, Defaults("my-wiki"), false); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	mustExist := []string{
		"keeba.config.yaml",
		"SCHEMA.md",
		"index.md",
		"log.md",
		"README.md",
		".gitignore",
		".mcp.json",
		"agents/git-ingest.md",
		"agents/slack-ingest.md",
		"entities/.gitkeep",
		"concepts/getting-started.md",
		"investigations/.gitkeep",
		"decisions/.gitkeep",
		".github/workflows/lint.yml",
		".github/workflows/index-rebuild.yml",
	}
	for _, p := range mustExist {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("expected %s, got %v", p, err)
		}
	}

	cfg, err := os.ReadFile(filepath.Join(out, "keeba.config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `name: "my-wiki"`) {
		t.Errorf("substitution failed: %s", cfg)
	}

	idx, err := os.ReadFile(filepath.Join(out, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(idx), "# my-wiki") {
		t.Errorf("index title not substituted: %s", idx)
	}
	if !strings.HasPrefix(string(idx), "---\n") {
		t.Errorf("index missing frontmatter")
	}
}

func TestScaffoldRefusesNonEmptyDir(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "preexisting"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Scaffold(out, Defaults("x"), false)
	if err == nil {
		t.Fatal("expected refusal on non-empty dir")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestScaffoldForceOverwrites(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "preexisting"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Scaffold(out, Defaults("x"), true); err != nil {
		t.Fatalf("Scaffold force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "SCHEMA.md")); err != nil {
		t.Errorf("SCHEMA.md missing after force: %v", err)
	}
}
