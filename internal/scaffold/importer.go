package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ImportResult summarizes a from-repo import.
type ImportResult struct {
	Imported []string // wiki-relative slugs that landed
	Skipped  []string // pre-existing files left untouched
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

	// 2. docs/** — flatten paths into hyphenated slugs under concepts/.
	docsRoot := filepath.Join(repoAbs, "docs")
	if st, err := os.Stat(docsRoot); err == nil && st.IsDir() {
		err := filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, walkErr error) error {
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
			slug := normalizeSlug("docs-" + strings.ReplaceAll(rel, "/", "-"))
			origin := "docs/" + rel + ".md"
			return writeImported(wikiRoot, path, slug, repoName, origin, &res)
		})
		if err != nil {
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

// wrapImported strips any incoming frontmatter, ensures the body has a
// level-1 title + summary line, and prepends keeba-compliant frontmatter
// plus Sources/See Also stubs at the end.
func wrapImported(body, repoName, origin string) string {
	body = stripIncomingFrontmatter(body)

	title := firstTitle(body)
	if title == "" {
		title = humanizeOrigin(origin)
	}
	summary := firstSummary(body)
	if summary == "" {
		summary = fmt.Sprintf("Imported from %s/%s — review and edit.", repoName, origin)
	}

	hasTitle := regexp.MustCompile(`(?m)^# `).MatchString(body)
	hasSummary := regexp.MustCompile(`(?m)^> `).MatchString(body)
	hasSources := regexp.MustCompile(`(?m)^## Sources\b`).MatchString(body)
	hasSeeAlso := regexp.MustCompile(`(?m)^## See Also\b`).MatchString(body)

	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "tags: [imported, %s]\n", repoName)
	fmt.Fprintf(&sb, "last_verified: %s\n", time.Now().UTC().Format("2006-01-02"))
	sb.WriteString("status: current\n")
	fmt.Fprintf(&sb, "cited_files: [\"%s/%s\"]\n", repoName, origin)
	sb.WriteString("---\n\n")

	if !hasTitle {
		fmt.Fprintf(&sb, "# %s\n\n", title)
	}
	if !hasSummary {
		fmt.Fprintf(&sb, "> %s\n\n", strings.TrimRight(summary, "."))
	}
	sb.WriteString(strings.TrimRight(body, "\n"))
	sb.WriteString("\n")
	if !hasSources {
		fmt.Fprintf(&sb, "\n## Sources\n\n- `%s/%s`\n", repoName, origin)
	}
	if !hasSeeAlso {
		sb.WriteString("\n## See Also\n\n- [[index]]\n")
	}
	return sb.String()
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

func firstTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func firstSummary(body string) string {
	lines := strings.Split(body, "\n")
	seen := 0
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		seen++
		if strings.HasPrefix(line, "> ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "> "))
		}
		// First non-title prose line — use it as the summary.
		if !strings.HasPrefix(line, "#") {
			s := strings.TrimSpace(line)
			if len(s) > 200 {
				s = s[:200] + "…"
			}
			return s
		}
		if seen >= 5 {
			break
		}
	}
	return ""
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
