// Package config loads keeba.config.yaml and exposes the merged
// configuration used by the lint, drift, and meta subsystems.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LintConfig governs the schema-lint rules.
type LintConfig struct {
	AllowedUppercaseFilenames []string `yaml:"allowed_uppercase_filenames"`
	SkipFilenames             []string `yaml:"skip_filenames"`
	SkipPathParts             []string `yaml:"skip_path_parts"`
	RequiredFrontmatterFields []string `yaml:"required_frontmatter_fields"`
	ValidStatusValues         []string `yaml:"valid_status_values"`
}

// DriftConfig governs citation-drift detection.
type DriftConfig struct {
	RepoPrefixes     []string `yaml:"repo_prefixes"`
	SkipPathPrefixes []string `yaml:"skip_path_prefixes"`
	GigarepoRoot     string   `yaml:"gigarepo_root"`
}

// IngestConfig caches per-source settings users would otherwise have to
// pass on every CLI invocation (notably which GitHub repo to ingest from).
type IngestConfig struct {
	GitHub IngestGitHubConfig `yaml:"github"`
}

// EncodingConfig holds per-page-type encoding pipeline specs. `keeba bench
// --encoding-grid-by-type --write-config` populates these; `keeba ingest`
// and `keeba sync` consume them at write time. Empty means "no encoding
// for that page type" — the page is written as raw markdown.
//
// Page types map 1-to-1 with bench.PageType:
//
//	function  — function/class definition pages (cited code files)
//	entity    — fact-heavy entity pages (bullet-list of properties)
//	narrative — flowing prose (default for anything else)
type EncodingConfig struct {
	Function  string `yaml:"function,omitempty"`
	Entity    string `yaml:"entity,omitempty"`
	Narrative string `yaml:"narrative,omitempty"`
}

// PipelineForType returns the configured pipeline spec for the named
// page-type ("function", "entity", "narrative"), or "" if none configured.
func (e EncodingConfig) PipelineForType(pageType string) string {
	switch pageType {
	case "function":
		return e.Function
	case "entity":
		return e.Entity
	case "narrative":
		return e.Narrative
	}
	return ""
}

// IngestGitHubConfig holds GitHub-specific ingest defaults.
type IngestGitHubConfig struct {
	// Repo is "owner/name". When set, `keeba ingest github` uses it without
	// prompting. Override at the CLI with --github-repo.
	Repo string `yaml:"repo"`
}

// KeebaConfig is the resolved configuration plus the wiki root path.
type KeebaConfig struct {
	SchemaVersion int            `yaml:"schema_version"`
	Name          string         `yaml:"name"`
	Purpose       string         `yaml:"purpose"`
	Lint          LintConfig     `yaml:"lint"`
	Drift         DriftConfig    `yaml:"drift"`
	Ingest        IngestConfig   `yaml:"ingest"`
	Encoding      EncodingConfig `yaml:"encoding,omitempty"`

	// WikiRoot is the directory that owns this config (or the directory the
	// caller asked us to treat as the wiki root). Always absolute.
	WikiRoot string `yaml:"-"`

	// ConfigPath is the absolute path to the loaded keeba.config.yaml, or
	// empty if no file was found and defaults were synthesized.
	ConfigPath string `yaml:"-"`
}

// Defaults returns a KeebaConfig populated with sensible defaults. Callers
// should overwrite WikiRoot before use.
func Defaults() KeebaConfig {
	return KeebaConfig{
		SchemaVersion: 1,
		Name:          "wiki",
		Lint: LintConfig{
			AllowedUppercaseFilenames: []string{"SCHEMA.md", "README.md", "QUERY_PATTERNS.md", "MEMORY.md"},
			SkipFilenames:             []string{"index.md", "log.md", "SCHEMA.md", "README.md", "QUERY_PATTERNS.md"},
			SkipPathParts:             []string{"_lint", "sources", ".github", ".pytest_cache", ".obsidian", "agents", "_xref", "_bench"},
			RequiredFrontmatterFields: []string{"tags", "last_verified", "status"},
			ValidStatusValues:         []string{"current", "draft", "archived", "deprecated", "proposed"},
		},
		Drift: DriftConfig{
			RepoPrefixes:     []string{},
			SkipPathPrefixes: []string{"wiki/", ".keeba/", "_bench/"},
			GigarepoRoot:     "..",
		},
	}
}

// FindCodeGraphRoot walks up from start looking for a directory that
// contains a compiled symbol graph at `.keeba/symbols.json`. Returns the
// absolute directory path on hit, or an empty string if the walk reaches
// the filesystem root without finding one. Used by `keeba mcp serve
// --wiki-root auto` so the MCP server resolves the right repo at startup
// based on Claude Code's launch cwd, rather than the cwd at install time.
//
// `.keeba/symbols.json` is the canonical signal because it's what the MCP
// server actually consumes (find_def, search_symbols, etc.). A repo with
// keeba.config.yaml but no compiled graph is no use to the MCP server.
func FindCodeGraphRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil {
		return ""
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		candidate := filepath.Join(abs, ".keeba", "symbols.json")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}

// FindWikiRoot walks up from start looking for a directory containing
// keeba.config.yaml. Returns the absolute directory path on hit, or an empty
// string if the walk reaches the filesystem root without finding one.
func FindWikiRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil {
		return ""
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		candidate := filepath.Join(abs, "keeba.config.yaml")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}

// Load returns the resolved configuration for the given wiki root. If
// wikiRoot is empty, the function walks up from the current working
// directory looking for keeba.config.yaml; if none is found, defaults are
// returned with WikiRoot set to the cwd.
func Load(wikiRoot string) (KeebaConfig, error) {
	if wikiRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return KeebaConfig{}, fmt.Errorf("getwd: %w", err)
		}
		if found := FindWikiRoot(cwd); found != "" {
			wikiRoot = found
		} else {
			wikiRoot = cwd
		}
	}
	abs, err := filepath.Abs(wikiRoot)
	if err != nil {
		return KeebaConfig{}, fmt.Errorf("abs(%q): %w", wikiRoot, err)
	}

	cfg := Defaults()
	cfg.WikiRoot = abs

	configPath := filepath.Join(abs, "keeba.config.yaml")
	data, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		// fall through
	case os.IsNotExist(err):
		return cfg, nil
	default:
		return cfg, fmt.Errorf("read %q: %w", configPath, err)
	}

	var loaded KeebaConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		return cfg, fmt.Errorf("parse %q: %w", configPath, err)
	}

	merge(&cfg, loaded)
	cfg.WikiRoot = abs
	cfg.ConfigPath = configPath
	return cfg, nil
}

func merge(dst *KeebaConfig, src KeebaConfig) {
	if src.SchemaVersion != 0 {
		dst.SchemaVersion = src.SchemaVersion
	}
	if src.Name != "" {
		dst.Name = src.Name
	}
	if src.Purpose != "" {
		dst.Purpose = src.Purpose
	}
	if src.Lint.AllowedUppercaseFilenames != nil {
		dst.Lint.AllowedUppercaseFilenames = src.Lint.AllowedUppercaseFilenames
	}
	if src.Lint.SkipFilenames != nil {
		dst.Lint.SkipFilenames = src.Lint.SkipFilenames
	}
	if src.Lint.SkipPathParts != nil {
		dst.Lint.SkipPathParts = src.Lint.SkipPathParts
	}
	if src.Lint.RequiredFrontmatterFields != nil {
		dst.Lint.RequiredFrontmatterFields = src.Lint.RequiredFrontmatterFields
	}
	if src.Lint.ValidStatusValues != nil {
		dst.Lint.ValidStatusValues = src.Lint.ValidStatusValues
	}
	if src.Drift.RepoPrefixes != nil {
		dst.Drift.RepoPrefixes = src.Drift.RepoPrefixes
	}
	if src.Drift.SkipPathPrefixes != nil {
		dst.Drift.SkipPathPrefixes = src.Drift.SkipPathPrefixes
	}
	if src.Drift.GigarepoRoot != "" {
		dst.Drift.GigarepoRoot = src.Drift.GigarepoRoot
	}
	if src.Ingest.GitHub.Repo != "" {
		dst.Ingest.GitHub.Repo = src.Ingest.GitHub.Repo
	}
	if src.Encoding.Function != "" {
		dst.Encoding.Function = src.Encoding.Function
	}
	if src.Encoding.Entity != "" {
		dst.Encoding.Entity = src.Encoding.Entity
	}
	if src.Encoding.Narrative != "" {
		dst.Encoding.Narrative = src.Encoding.Narrative
	}
}

// SaveEncoding persists per-page-type encoding pipeline specs into
// keeba.config.yaml at the wiki root. Empty values clear the field.
// Operates on a yaml.Node tree so unknown / future fields the user has
// in their config are preserved.
func (c KeebaConfig) SaveEncoding(enc EncodingConfig) error {
	target := filepath.Join(c.WikiRoot, "keeba.config.yaml")
	root, err := loadYAMLDoc(target)
	if err != nil {
		return err
	}
	doc := docMap(root)
	if doc == nil {
		return fmt.Errorf("%s is not a YAML mapping at the top level", target)
	}
	encNode := ensureMapping(doc, "encoding")
	setOrClearScalar(encNode, "function", enc.Function)
	setOrClearScalar(encNode, "entity", enc.Entity)
	setOrClearScalar(encNode, "narrative", enc.Narrative)

	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(target, out, 0o644)
}

// SaveGitHubRepo persists ingest.github.repo into keeba.config.yaml at the
// wiki root. Operates on a yaml.Node tree so unknown / future fields the
// user has in their config are preserved (round-tripping through the
// KeebaConfig struct would drop them).
func (c KeebaConfig) SaveGitHubRepo(repo string) error {
	target := filepath.Join(c.WikiRoot, "keeba.config.yaml")
	root, err := loadYAMLDoc(target)
	if err != nil {
		return err
	}
	doc := docMap(root)
	if doc == nil {
		return fmt.Errorf("%s is not a YAML mapping at the top level", target)
	}
	ingest := ensureMapping(doc, "ingest")
	github := ensureMapping(ingest, "github")
	setScalar(github, "repo", repo)

	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(target, out, 0o644)
}

// loadYAMLDoc reads target and returns the top-level yaml.Node tree, or a
// fresh empty mapping document if the file is missing/empty. Used by
// SaveEncoding / SaveGitHubRepo to round-trip through the AST so user
// fields the struct doesn't know about survive round-trips.
func loadYAMLDoc(target string) (*yaml.Node, error) {
	root := &yaml.Node{}
	if data, err := os.ReadFile(target); err == nil && len(data) > 0 {
		if err := yaml.Unmarshal(data, root); err != nil {
			return nil, fmt.Errorf("parse %s: %w", target, err)
		}
	}
	if root.Kind == 0 {
		root = &yaml.Node{
			Kind: yaml.DocumentNode,
			Content: []*yaml.Node{
				{Kind: yaml.MappingNode},
			},
		}
	}
	return root, nil
}

// setOrClearScalar sets parent[key] to a scalar value, or removes the key
// if value is empty. Lets SaveEncoding leave unset page-types absent from
// the config rather than emitting empty strings.
func setOrClearScalar(parent *yaml.Node, key, value string) {
	if value == "" {
		// Remove the key if present.
		for i := 0; i+1 < len(parent.Content); i += 2 {
			if parent.Content[i].Value == key {
				parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
				return
			}
		}
		return
	}
	setScalar(parent, key, value)
}

// docMap returns the top-level mapping node for either a DocumentNode or a
// bare MappingNode. nil if neither.
func docMap(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

// ensureMapping returns the mapping value for key under parent, creating
// it if missing.
func ensureMapping(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			child := parent.Content[i+1]
			if child.Kind == yaml.MappingNode {
				return child
			}
			// Replace whatever's there with an empty mapping.
			child.Kind = yaml.MappingNode
			child.Tag = "!!map"
			child.Content = nil
			return child
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, keyNode, valNode)
	return valNode
}

// setScalar sets parent[key] to a string scalar, replacing any existing
// value at that key.
func setScalar(parent *yaml.Node, key, value string) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1].Kind = yaml.ScalarNode
			parent.Content[i+1].Tag = "!!str"
			parent.Content[i+1].Value = value
			parent.Content[i+1].Content = nil
			return
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

// GigarepoRoot returns the absolute path to the directory citation paths are
// resolved against.
func (c KeebaConfig) GigarepoRoot() string {
	if filepath.IsAbs(c.Drift.GigarepoRoot) {
		return c.Drift.GigarepoRoot
	}
	return filepath.Clean(filepath.Join(c.WikiRoot, c.Drift.GigarepoRoot))
}
