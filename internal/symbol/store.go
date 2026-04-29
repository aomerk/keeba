package symbol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// IndexDir is the per-repo directory keeba writes to. Conventionally
	// under the wiki root for keeba init flows; under the repo root for
	// `keeba compile <repo>`.
	IndexDir = ".keeba"
	// SymbolsFile is the JSON serialization of the extracted graph.
	// Flat array of Symbol — easy to grep, easy for the MCP layer to
	// stream, no sqlite dep.
	SymbolsFile = "symbols.json"
)

// Index is the on-disk format. Wraps the symbol list with a small header
// so future versions can bump the schema without breaking parse.
type Index struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	RepoRoot      string    `json:"repo_root"`
	NumFiles      int       `json:"num_files"`
	NumSymbols    int       `json:"num_symbols"`
	Symbols       []Symbol  `json:"symbols"`
}

// IndexPath returns the canonical .keeba/symbols.json path for the
// given target directory.
func IndexPath(targetDir string) string {
	return filepath.Join(targetDir, IndexDir, SymbolsFile)
}

// Save writes idx to .keeba/symbols.json under targetDir, creating the
// directory if needed. Output is pretty-printed for `git diff`-ability.
func Save(targetDir string, idx Index) error {
	path := IndexPath(targetDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads .keeba/symbols.json. Returns an os.IsNotExist-classifiable
// error if the file is missing — callers can use that to suggest
// `keeba compile`.
func Load(targetDir string) (Index, error) {
	path := IndexPath(targetDir)
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return Index{}, fmt.Errorf("read %s: %w", path, err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return idx, nil
}

// Compile is the high-level entry point: walk repoRoot, extract every
// symbol, and write the index under writeRoot/.keeba/symbols.json.
// repoRoot and writeRoot are usually the same for `keeba compile <repo>`,
// but can differ when compiling a source repo into a wiki dir.
func Compile(repoRoot, writeRoot string) (Index, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return Index{}, err
	}
	syms, err := ExtractRepo(abs)
	if err != nil {
		return Index{}, err
	}

	files := map[string]struct{}{}
	for _, s := range syms {
		files[s.File] = struct{}{}
	}

	idx := Index{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		RepoRoot:      abs,
		NumFiles:      len(files),
		NumSymbols:    len(syms),
		Symbols:       syms,
	}
	if err := Save(writeRoot, idx); err != nil {
		return idx, err
	}
	return idx, nil
}

// CountByLanguage tallies symbols per language tag — used by the CLI to
// print a per-language breakdown after compile.
func (idx Index) CountByLanguage() map[string]int {
	out := map[string]int{}
	for _, s := range idx.Symbols {
		out[s.Language]++
	}
	return out
}

// CountByKind tallies symbols per kind (function, method, class, …).
func (idx Index) CountByKind() map[string]int {
	out := map[string]int{}
	for _, s := range idx.Symbols {
		out[s.Kind]++
	}
	return out
}
