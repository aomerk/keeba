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

// KeebaConfig is the resolved configuration plus the wiki root path.
type KeebaConfig struct {
	SchemaVersion int         `yaml:"schema_version"`
	Name          string      `yaml:"name"`
	Purpose       string      `yaml:"purpose"`
	Lint          LintConfig  `yaml:"lint"`
	Drift         DriftConfig `yaml:"drift"`

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
}

// GigarepoRoot returns the absolute path to the directory citation paths are
// resolved against.
func (c KeebaConfig) GigarepoRoot() string {
	if filepath.IsAbs(c.Drift.GigarepoRoot) {
		return c.Drift.GigarepoRoot
	}
	return filepath.Clean(filepath.Join(c.WikiRoot, c.Drift.GigarepoRoot))
}
