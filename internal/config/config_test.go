package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestFindCodeGraphRootWalksUp(t *testing.T) {
	// Symbol graph 3 levels up — the case `keeba mcp serve --wiki-root
	// auto` actually has to handle when Claude Code is launched in a
	// nested subdir of an indexed repo.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".keeba"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(root, ".keeba", "symbols.json"), "{}\n")
	deep := filepath.Join(root, "pkg", "indexer", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := FindCodeGraphRoot(deep)
	want, _ := filepath.EvalSymlinks(root)
	gotEval, _ := filepath.EvalSymlinks(got)
	if gotEval != want {
		t.Fatalf("FindCodeGraphRoot = %q, want %q", got, root)
	}
}

func TestFindCodeGraphRootMissReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := FindCodeGraphRoot(dir); got != "" {
		t.Fatalf("FindCodeGraphRoot on dir without .keeba = %q, want empty", got)
	}
}

func TestFindCodeGraphRootIgnoresDirEntry(t *testing.T) {
	// `.keeba/symbols.json` must be a regular file, not a directory.
	// Belt-and-suspenders against weird filesystems where a stray dir
	// with the right name could otherwise short-circuit the walk.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".keeba", "symbols.json"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := FindCodeGraphRoot(dir); got != "" {
		t.Fatalf("FindCodeGraphRoot when symbols.json is a dir = %q, want empty (regular file required)", got)
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

func TestEncodingPipelineForType(t *testing.T) {
	enc := EncodingConfig{
		Function:  "glossary,structural-card",
		Entity:    "dense-tuple",
		Narrative: "glossary,caveman",
	}
	cases := map[string]string{
		"function":  "glossary,structural-card",
		"entity":    "dense-tuple",
		"narrative": "glossary,caveman",
		"unknown":   "",
		"":          "",
	}
	for k, want := range cases {
		t.Run(k, func(t *testing.T) {
			if got := enc.PipelineForType(k); got != want {
				t.Errorf("PipelineForType(%q) = %q, want %q", k, got, want)
			}
		})
	}
}

func TestLoadEncodingFromYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keeba.config.yaml"), `
name: test
encoding:
  function: glossary,structural-card
  narrative: glossary,caveman
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Encoding.Function != "glossary,structural-card" {
		t.Errorf("Encoding.Function = %q", cfg.Encoding.Function)
	}
	if cfg.Encoding.Narrative != "glossary,caveman" {
		t.Errorf("Encoding.Narrative = %q", cfg.Encoding.Narrative)
	}
	if cfg.Encoding.Entity != "" {
		t.Errorf("Encoding.Entity should be empty, got %q", cfg.Encoding.Entity)
	}
}

func TestSaveEncodingRoundTripsExistingFields(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keeba.config.yaml")
	// Pre-existing config with fields the struct doesn't model
	// (custom_field) plus a known field (name).
	writeFile(t, target, `
name: hand-written
custom_field: must-survive
ingest:
  github:
    repo: aomerk/keeba
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := cfg.SaveEncoding(EncodingConfig{
		Function:  "glossary,structural-card",
		Narrative: "glossary,caveman",
	}); err != nil {
		t.Fatalf("SaveEncoding: %v", err)
	}

	out, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"name: hand-written",
		"custom_field: must-survive",
		"repo: aomerk/keeba",
		"function: glossary,structural-card",
		"narrative: glossary,caveman",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
	if strings.Contains(s, "entity:") {
		t.Errorf("empty Entity should not be persisted, got:\n%s", s)
	}
}

func TestSaveEncodingClearsRemovedFields(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keeba.config.yaml")
	writeFile(t, target, `
name: x
encoding:
  function: glossary,structural-card
  entity: dense-tuple
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Save with only function set; entity should be cleared.
	if err := cfg.SaveEncoding(EncodingConfig{
		Function: "structural-card",
	}); err != nil {
		t.Fatalf("SaveEncoding: %v", err)
	}

	out, _ := os.ReadFile(target)
	s := string(out)
	if !strings.Contains(s, "function: structural-card") {
		t.Errorf("expected updated function, got:\n%s", s)
	}
	if strings.Contains(s, "entity:") {
		t.Errorf("entity should be cleared, got:\n%s", s)
	}
}
