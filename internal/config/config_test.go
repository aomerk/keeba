package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDefaultsHaveSaneValues(t *testing.T) {
	d := Defaults()
	if d.SchemaVersion != 1 {
		t.Fatalf("schema_version: got %d", d.SchemaVersion)
	}
	if len(d.Lint.RequiredFrontmatterFields) == 0 {
		t.Fatal("required_frontmatter_fields empty")
	}
	if len(d.Lint.ValidStatusValues) == 0 {
		t.Fatal("valid_status_values empty")
	}
	if d.Drift.GigarepoRoot != ".." {
		t.Fatalf("gigarepo_root default: %q", d.Drift.GigarepoRoot)
	}
}

func TestLoadNoFileFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WikiRoot != dir {
		t.Fatalf("WikiRoot: got %q, want %q", cfg.WikiRoot, dir)
	}
	if cfg.ConfigPath != "" {
		t.Fatalf("ConfigPath should be empty when no config file exists, got %q", cfg.ConfigPath)
	}
	if got := len(cfg.Lint.RequiredFrontmatterFields); got != 3 {
		t.Fatalf("default required fields count = %d", got)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keeba.config.yaml"), `
schema_version: 2
name: my-wiki
purpose: "test"
lint:
  required_frontmatter_fields: ["tags"]
  valid_status_values: ["current"]
drift:
  repo_prefixes: ["my-app/", "my-infra/"]
  gigarepo_root: "../parent"
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SchemaVersion != 2 {
		t.Fatalf("schema_version: %d", cfg.SchemaVersion)
	}
	if cfg.Name != "my-wiki" {
		t.Fatalf("name: %q", cfg.Name)
	}
	if got := cfg.Lint.RequiredFrontmatterFields; len(got) != 1 || got[0] != "tags" {
		t.Fatalf("required: %v", got)
	}
	if got := cfg.Drift.RepoPrefixes; len(got) != 2 || got[0] != "my-app/" {
		t.Fatalf("repo_prefixes: %v", got)
	}
	if !filepath.IsAbs(cfg.GigarepoRoot()) {
		t.Fatalf("GigarepoRoot should be absolute, got %q", cfg.GigarepoRoot())
	}
}

func TestLoadKeepsDefaultsForUnsetSections(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keeba.config.yaml"), `
name: minimal
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != "minimal" {
		t.Fatalf("name not applied: %q", cfg.Name)
	}
	if got := len(cfg.Lint.RequiredFrontmatterFields); got != 3 {
		t.Fatalf("defaults lost: required field count = %d", got)
	}
	if got := cfg.Drift.GigarepoRoot; got != ".." {
		t.Fatalf("default gigarepo_root lost: %q", got)
	}
}

func TestFindWikiRootWalksUp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keeba.config.yaml"), "name: x\n")
	deep := filepath.Join(root, "concepts", "nested")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := FindWikiRoot(deep)
	want, _ := filepath.EvalSymlinks(root)
	gotEval, _ := filepath.EvalSymlinks(got)
	if gotEval != want {
		t.Fatalf("FindWikiRoot = %q, want %q", got, root)
	}
}

func TestFindWikiRootMissReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := FindWikiRoot(dir); got != "" {
		t.Fatalf("FindWikiRoot on empty dir = %q, want empty", got)
	}
}

func TestLoadInvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keeba.config.yaml"), "name: [unclosed\n")
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error from invalid yaml")
	}
}
