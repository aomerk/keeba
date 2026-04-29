// Package symbol extracts the symbol graph from a code repo so AI tools
// can answer "where is X defined / how is it called" without grep loops.
//
// This is the §10 pivot's foundation: instead of building a wiki and
// indexing pages, we compile the repo's symbols once and serve structured
// MCP queries (find_def, find_callers, summary) over them. Keeps Claude
// Code / Cursor / Codex small and fast.
//
// Pure Go, no CGO, no runtime deps. Go files use go/parser; everything
// else uses pattern-based extraction (extract_regex.go) calibrated for
// Python / JavaScript / TypeScript / Rust / Java / C / Ruby.
package symbol

import (
	"os"
	"path/filepath"
	"strings"
)

// Symbol is one extracted definition: function, class, method, type, etc.
// Stable JSON shape so .keeba/symbols.json is consumed by the MCP layer
// and other tooling without re-parsing.
type Symbol struct {
	// Name is the bare identifier — what an agent calls find_def("Name") on.
	Name string `json:"name"`
	// Kind is "function" | "method" | "class" | "type" | "interface" | "const" | "var".
	Kind string `json:"kind"`
	// File is the repo-relative path.
	File string `json:"file"`
	// StartLine is 1-based; EndLine is the last line of the symbol body
	// (best-effort for regex extractors — Go AST gives exact bounds).
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
	// Signature is the declaration line, trimmed.
	Signature string `json:"signature"`
	// Doc is the leading docstring or comment block, if any. Already
	// stripped of comment markers.
	Doc string `json:"doc,omitempty"`
	// Receiver is the type a method hangs off (Go: `(s *Server)` → "Server",
	// Python: enclosing class, Rust: enclosing impl). Empty for free
	// functions.
	Receiver string `json:"receiver,omitempty"`
	// Language is the source language tag ("go", "py", "ts", …) — useful
	// for MCP filtering and for the bench's per-type partition.
	Language string `json:"language"`
}

// Extractor is the language-specific extractor contract.
type Extractor interface {
	// Extract returns all symbols defined in src. file is the repo-relative
	// path used to populate Symbol.File.
	Extract(file string, src []byte) ([]Symbol, error)
}

// Language → extractor. Resolved lazily by extractFor; nil means
// "language not supported, skip silently".
var extractors = map[string]Extractor{}

func init() {
	extractors["go"] = goExtractor{}

	// Regex extractors share one struct with per-language regexes.
	for lang, rx := range regexExtractorsByLang {
		extractors[lang] = regexExtractor{lang: lang, rx: rx}
	}
}

func detectLanguage(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".go":
		return "go"
	case ".py", ".pyx":
		return "py"
	case ".js", ".mjs", ".cjs":
		return "js"
	case ".ts", ".tsx":
		return "ts"
	case ".jsx":
		return "js"
	case ".rs":
		return "rs"
	case ".java":
		return "java"
	case ".kt":
		return "kt"
	case ".rb":
		return "rb"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	}
	return ""
}

// extractFile dispatches one file to its language extractor. Returns
// nil, nil for unsupported languages so callers can ignore unknown files.
func extractFile(path string, src []byte) ([]Symbol, error) {
	lang := detectLanguage(path)
	ex, ok := extractors[lang]
	if !ok {
		return nil, nil
	}
	return ex.Extract(path, src)
}

// ExtractRepo walks repoRoot and returns every symbol in every supported
// file. Hidden directories (.git, .venv) and big-binary directories
// (node_modules, vendor, target, dist, build) are skipped. Files larger
// than 1 MiB are skipped to keep extraction fast on huge files.
func ExtractRepo(repoRoot string) ([]Symbol, error) {
	const maxFileBytes = 1 << 20

	skip := map[string]struct{}{
		".git": {}, ".hg": {}, ".svn": {},
		"node_modules": {}, "vendor": {}, ".venv": {}, "venv": {}, "env": {},
		"__pycache__": {}, ".tox": {}, ".pytest_cache": {}, ".mypy_cache": {},
		".ruff_cache": {}, "dist": {}, "build": {}, ".next": {}, ".nuxt": {},
		"target": {}, ".idea": {}, ".vscode": {}, ".keeba": {}, ".cache": {},
	}

	var out []Symbol
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if _, drop := skip[name]; drop {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && path != repoRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > maxFileBytes {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			return nil // skip unreadable files
		}
		syms, err := extractFile(rel, src)
		if err != nil {
			// Per-file extraction errors must not abort the whole walk —
			// agents would rather get partial graph than nothing.
			return nil
		}
		out = append(out, syms...)
		return nil
	})
	return out, err
}
