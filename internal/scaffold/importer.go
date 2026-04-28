package scaffold

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ImportResult summarizes a from-repo import or sync.
type ImportResult struct {
	Imported []string // wiki-relative slugs that landed (created or updated)
	Skipped  []string // pre-existing files left untouched
	Edited   []string // sync only — pages skipped because the user edited them
}

// topLevelImports lists exact filenames in the repo root we always pull.
var topLevelImports = []string{
	"README.md", "CLAUDE.md", "ARCHITECTURE.md",
	"CONTRIBUTING.md", "ROADMAP.md", "CHANGELOG.md",
}

// ImportFromRepo seeds the scaffolded wiki with markdown files lifted from
// repoPath: top-level docs (README.md, CLAUDE.md, …) plus everything under
// docs/. Each imported file is wrapped with frontmatter that satisfies
// `keeba lint`. Existing wiki pages are never overwritten.
func ImportFromRepo(wikiRoot, repoPath string, repoName string) (ImportResult, error) {
	res := ImportResult{}
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return res, fmt.Errorf("abs(%q): %w", repoPath, err)
	}
	if st, err := os.Stat(repoAbs); err != nil || !st.IsDir() {
		return res, fmt.Errorf("repo path %q is not a directory", repoAbs)
	}
	if repoName == "" {
		repoName = filepath.Base(repoAbs)
	}

	// 1. Top-level files.
	for _, name := range topLevelImports {
		src := filepath.Join(repoAbs, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		slug := normalizeSlug(strings.TrimSuffix(strings.ToLower(name), ".md"))
		if err := writeImported(wikiRoot, src, slug, repoName, name, &res); err != nil {
			return res, err
		}
	}

	// 2. Doc directories — `docs/`, `doc/`, `documentation/`. Real repos
	// use one or the other (llm.c uses singular `doc/`); we walk all three.
	for _, dirName := range []string{"docs", "doc", "documentation"} {
		docsRoot := filepath.Join(repoAbs, dirName)
		st, err := os.Stat(docsRoot)
		if err != nil || !st.IsDir() {
			continue
		}
		err = filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(d.Name()) != ".md" {
				return nil
			}
			rel, _ := filepath.Rel(docsRoot, path)
			rel = filepath.ToSlash(strings.TrimSuffix(rel, ".md"))
			slug := normalizeSlug(dirName + "-" + strings.ReplaceAll(rel, "/", "-"))
			origin := dirName + "/" + rel + ".md"
			return writeImported(wikiRoot, path, slug, repoName, origin, &res)
		})
		if err != nil {
			return res, err
		}
	}

	// 3. Nested README.md files at <subdir>/README.md (one level deep). llm.c
	// has scripts/README.md, many monorepos have similar layouts.
	entries, err := os.ReadDir(repoAbs)
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "docs" || name == "doc" ||
			name == "documentation" || name == "node_modules" || name == "vendor" {
			continue
		}
		nested := filepath.Join(repoAbs, name, "README.md")
		if _, err := os.Stat(nested); err != nil {
			continue
		}
		slug := normalizeSlug(name + "-readme")
		origin := name + "/README.md"
		if err := writeImported(wikiRoot, nested, slug, repoName, origin, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

var slugSafe = regexp.MustCompile(`[^a-z0-9-]+`)

func normalizeSlug(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = slugSafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "imported"
	}
	return s
}

func writeImported(wikiRoot, srcPath, slug, repoName, origin string, res *ImportResult) error {
	dest := filepath.Join(wikiRoot, "concepts", slug+".md")
	if _, err := os.Stat(dest); err == nil {
		res.Skipped = append(res.Skipped, "concepts/"+slug+".md")
		return nil
	}
	body, err := os.ReadFile(srcPath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	wrapped := wrapImported(string(body), repoName, origin)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dest, []byte(wrapped), 0o644); err != nil {
		return err
	}
	res.Imported = append(res.Imported, "concepts/"+slug+".md")
	return nil
}

// SyncFromRepo refreshes the wiki from a source repo. Behaviour per file:
//
//   - destination doesn't exist → write (same as ImportFromRepo).
//   - destination exists and has frontmatter `keeba_pristine_hash` matching
//     the on-disk body's hash → pristine; safe to overwrite with the new
//     import.
//   - destination exists with no hash, or with a mismatched hash → user
//     touched it. Leave alone, return slug in Edited.
//
// Idempotent. Re-running on an unchanged source is a no-op (page bodies
// match the recorded hash, but the new wrapper output is byte-identical to
// what's on disk modulo last_verified, so the file still gets rewritten —
// `keeba meta --check` would no-op afterward).
func SyncFromRepo(wikiRoot, repoPath, repoName string) (ImportResult, error) {
	res := ImportResult{}
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return res, fmt.Errorf("abs(%q): %w", repoPath, err)
	}
	if st, err := os.Stat(repoAbs); err != nil || !st.IsDir() {
		return res, fmt.Errorf("repo path %q is not a directory", repoAbs)
	}
	if repoName == "" {
		repoName = filepath.Base(repoAbs)
	}

	walk := func(srcPath, slug, origin string) error {
		return upsertImported(wikiRoot, srcPath, slug, repoName, origin, &res)
	}

	for _, name := range topLevelImports {
		src := filepath.Join(repoAbs, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		slug := normalizeSlug(strings.TrimSuffix(strings.ToLower(name), ".md"))
		if err := walk(src, slug, name); err != nil {
			return res, err
		}
	}
	for _, dirName := range []string{"docs", "doc", "documentation"} {
		docsRoot := filepath.Join(repoAbs, dirName)
		st, err := os.Stat(docsRoot)
		if err != nil || !st.IsDir() {
			continue
		}
		err = filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || filepath.Ext(d.Name()) != ".md" {
				return nil
			}
			rel, _ := filepath.Rel(docsRoot, path)
			rel = filepath.ToSlash(strings.TrimSuffix(rel, ".md"))
			slug := normalizeSlug(dirName + "-" + strings.ReplaceAll(rel, "/", "-"))
			return walk(path, slug, dirName+"/"+rel+".md")
		})
		if err != nil {
			return res, err
		}
	}
	entries, err := os.ReadDir(repoAbs)
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "docs" || name == "doc" ||
			name == "documentation" || name == "node_modules" || name == "vendor" {
			continue
		}
		nested := filepath.Join(repoAbs, name, "README.md")
		if _, err := os.Stat(nested); err != nil {
			continue
		}
		if err := walk(nested, normalizeSlug(name+"-readme"), name+"/README.md"); err != nil {
			return res, err
		}
	}
	return res, nil
}

// upsertImported is the sync version of writeImported: pristine pages get
// overwritten; edited pages get skipped.
func upsertImported(wikiRoot, srcPath, slug, repoName, origin string, res *ImportResult) error {
	dest := filepath.Join(wikiRoot, "concepts", slug+".md")
	srcBody, err := os.ReadFile(srcPath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	newWrapped := wrapImported(string(srcBody), repoName, origin)

	existing, err := os.ReadFile(dest) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
				return mkErr
			}
			if wErr := os.WriteFile(dest, []byte(newWrapped), 0o644); wErr != nil {
				return wErr
			}
			res.Imported = append(res.Imported, "concepts/"+slug+".md")
			return nil
		}
		return err
	}

	if isPristineImport(string(existing)) {
		if err := os.WriteFile(dest, []byte(newWrapped), 0o644); err != nil {
			return err
		}
		res.Imported = append(res.Imported, "concepts/"+slug+".md")
		return nil
	}
	res.Edited = append(res.Edited, "concepts/"+slug+".md")
	return nil
}

// isPristineImport returns true when the page was created by a previous
// keeba import and has not been hand-edited since: it must carry the
// `keeba_pristine_hash` frontmatter field, and the body's current hash must
// match that recorded value.
func isPristineImport(text string) bool {
	fmStr, body := splitFrontmatter(text)
	hash := readPristineHash(fmStr)
	if hash == "" {
		return false
	}
	got := sha256.Sum256([]byte(body))
	return hex.EncodeToString(got[:]) == hash
}

// splitFrontmatter returns (frontmatter-yaml, body). The body has any
// leading blank lines stripped so the same body string is reproducible
// whether you build it pre-write or read it back post-write — important
// because keeba_pristine_hash compares against this canonical form.
func splitFrontmatter(text string) (string, string) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text
	}
	idx := strings.Index(text[4:], "\n---\n")
	if idx == -1 {
		return "", text
	}
	return text[4 : 4+idx], strings.TrimLeft(text[4+idx+5:], "\n")
}

var rePristineHash = regexp.MustCompile(`(?m)^keeba_pristine_hash:\s*"?([a-f0-9]{64})"?\s*$`)

func readPristineHash(fm string) string {
	m := rePristineHash.FindStringSubmatch(fm)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

// bodyHash returns the sha256 hex of body, used as keeba_pristine_hash.
func bodyHash(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// wrapImported produces a keeba-lint-compliant page from an arbitrary
// markdown source. It always emits frontmatter → title → summary → body in
// that order, and appends Sources/See Also if the source didn't have them.
//
// To avoid duplicating the title, the body's own `# Title` line (if any) is
// stripped before being re-emitted under the canonical title.
func wrapImported(body, repoName, origin string) string {
	body = stripIncomingFrontmatter(body)

	title, bodyAfterTitle := extractAndStripTitle(body)
	if title == "" {
		title = humanizeOrigin(origin)
	}

	summary, bodyAfterSummary := extractAndStripSummary(bodyAfterTitle)
	if summary == "" {
		// Synthesize a summary from the first prose line if the source had no
		// `> blockquote`. Falls through to a generic placeholder.
		summary = synthesizeSummary(bodyAfterSummary)
	}
	if summary == "" {
		summary = fmt.Sprintf("Imported from %s/%s — review and edit", repoName, origin)
	}

	hasSources := regexp.MustCompile(`(?m)^## Sources\b`).MatchString(bodyAfterSummary)
	hasSeeAlso := regexp.MustCompile(`(?m)^## See Also\b`).MatchString(bodyAfterSummary)

	// Build the body first so we can hash it for keeba_pristine_hash.
	var bodyBuf strings.Builder
	fmt.Fprintf(&bodyBuf, "# %s\n\n", title)
	fmt.Fprintf(&bodyBuf, "> %s\n\n", strings.TrimSpace(summary))
	bodyBuf.WriteString(strings.TrimRight(bodyAfterSummary, "\n"))
	bodyBuf.WriteString("\n")
	if !hasSources {
		fmt.Fprintf(&bodyBuf, "\n## Sources\n\n- `%s/%s`\n", repoName, origin)
	}
	if !hasSeeAlso {
		bodyBuf.WriteString("\n## See Also\n\n- [[index]]\n")
	}
	bodyStr := bodyBuf.String()

	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "tags: [imported, %s]\n", repoName)
	fmt.Fprintf(&sb, "last_verified: %s\n", time.Now().UTC().Format("2006-01-02"))
	sb.WriteString("status: current\n")
	fmt.Fprintf(&sb, "cited_files: [\"%s/%s\"]\n", repoName, origin)
	// Pristine hash lets `keeba sync` tell pristine pages from edited ones.
	// Stripping or modifying this hash effectively "claims" the page as
	// hand-curated and keeba sync will skip it.
	fmt.Fprintf(&sb, "keeba_pristine_hash: %s\n", bodyHash(bodyStr))
	sb.WriteString("---\n\n")
	sb.WriteString(bodyStr)
	return sb.String()
}

// extractAndStripTitle returns (title, body-without-that-title-line). If the
// body has no `# Heading`, returns ("", body unchanged).
func extractAndStripTitle(body string) (string, string) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			// Drop the title line and any leading blank line right after it.
			rest := append([]string{}, lines[:i]...)
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			rest = append(rest, lines[j:]...)
			return title, strings.Join(rest, "\n")
		}
	}
	return "", body
}

// extractAndStripSummary returns (summary, body-without-summary). Looks for
// a `> ` line within the first 5 non-empty lines and removes it from the
// body so we can re-emit it in canonical position.
func extractAndStripSummary(body string) (string, string) {
	lines := strings.Split(body, "\n")
	seen := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		seen++
		if strings.HasPrefix(line, "> ") {
			summary := strings.TrimSpace(strings.TrimPrefix(line, "> "))
			rest := append([]string{}, lines[:i]...)
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			rest = append(rest, lines[j:]...)
			return summary, strings.Join(rest, "\n")
		}
		if seen >= 5 {
			break
		}
	}
	return "", body
}

// synthesizeSummary picks a one-line summary from the first prose paragraph
// of body. Returns "" when the body has no usable prose.
func synthesizeSummary(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "```") || strings.HasPrefix(s, "|") {
			continue
		}
		// First sentence, capped at 200 chars.
		if idx := strings.IndexAny(s, ".!?"); idx > 20 {
			s = s[:idx+1]
		}
		if len(s) > 200 {
			s = s[:197] + "…"
		}
		return s
	}
	return ""
}

func stripIncomingFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	idx := strings.Index(body[4:], "\n---\n")
	if idx == -1 {
		return body
	}
	return strings.TrimLeft(body[4+idx+5:], "\n")
}

func humanizeOrigin(origin string) string {
	stem := strings.TrimSuffix(filepath.Base(origin), ".md")
	stem = strings.ReplaceAll(stem, "-", " ")
	stem = strings.ReplaceAll(stem, "_", " ")
	if stem == "" {
		return "Imported page"
	}
	// Title-case first letter.
	return strings.ToUpper(stem[:1]) + stem[1:]
}
