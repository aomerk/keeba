// Package lint implements keeba's wiki schema rules, citation drift
// detection, and the meta + xref index builders.
package lint

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/aomerk/keeba/internal/config"
)

// Severity classifies a Violation. "error" fails CI; "warning" surfaces in
// reports without failing.
type Severity string

// Severity values in priority order (errors fail CI; warnings inform).
const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
)

// Violation is a single rule failure on a single page.
type Violation struct {
	File     string
	Line     int // 0 means "no specific line"
	Rule     string
	Severity Severity
	Message  string
	Autofix  bool
}

var (
	wikilinkRe       = regexp.MustCompile(`\[\[([^\]|#]+?)(?:\|[^\]]+)?(?:#[^\]]+)?\]\]`)
	datedFilenameRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)
	filenameStemRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	headingTitleRe   = regexp.MustCompile(`(?m)^# (.+)$`)
	sourcesHeaderRe  = regexp.MustCompile(`(?m)^## Sources\b`)
	seeAlsoHeaderRe  = regexp.MustCompile(`(?m)^## See Also\b`)
	inlineCodeRe     = regexp.MustCompile("`[^`\n]*`")
	fencedCodeOpenRe = regexp.MustCompile("(?m)^[\t ]*```")
)

// readFile is overridable for tests but uses os.ReadFile in practice.
var readFile = func(p string) (string, error) {
	b, err := readBytes(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckTitle verifies the page starts with a level-1 heading.
func CheckTitle(path, body string) []Violation {
	stripped := StripFrontmatter(body)
	for _, line := range strings.Split(stripped, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return nil
		}
		break
	}
	return []Violation{{
		File: path, Line: 1, Rule: "missing-title", Severity: SevError,
		Message: "Page must start with `# Title` (level-1 heading).",
	}}
}

// CheckSummary verifies a `> ...` summary line within the first 5 non-empty
// lines after the title.
func CheckSummary(path, body string) []Violation {
	stripped := StripFrontmatter(body)
	lines := strings.Split(stripped, "\n")
	seen := 0
	titleSkipped := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if !titleSkipped {
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			titleSkipped = true
			continue
		}
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		seen++
		if strings.HasPrefix(trimmed, "> ") {
			return nil
		}
		if seen >= 5 {
			break
		}
	}
	return []Violation{{
		File: path, Rule: "missing-summary", Severity: SevError,
		Message: "Page must include `> One-line summary.` within the first 5 lines after the title.",
	}}
}

// CheckSources verifies a `## Sources` section is present.
func CheckSources(path, body string) []Violation {
	if sourcesHeaderRe.MatchString(body) {
		return nil
	}
	return []Violation{{
		File: path, Rule: "missing-sources", Severity: SevError,
		Message: "Page must include a `## Sources` section.",
	}}
}

// CheckSeeAlso verifies a `## See Also` section is present.
func CheckSeeAlso(path, body string) []Violation {
	if seeAlsoHeaderRe.MatchString(body) {
		return nil
	}
	return []Violation{{
		File: path, Rule: "missing-see-also", Severity: SevError,
		Message: "Page must include a `## See Also` section.",
	}}
}

// CheckWikilinks verifies every `[[link]]` resolves to a markdown file in
// the wiki tree. Links inside fenced or inline code blocks are ignored.
func CheckWikilinks(path, body, wikiRoot string) []Violation {
	cleaned := stripCodeRegions(body)
	idx, err := buildPageIndex(wikiRoot)
	if err != nil {
		return []Violation{{
			File: path, Rule: "wiki-walk-failed", Severity: SevWarning,
			Message: fmt.Sprintf("could not walk wiki root %q: %v", wikiRoot, err),
		}}
	}
	var violations []Violation
	for _, m := range wikilinkRe.FindAllStringSubmatchIndex(cleaned, -1) {
		full := cleaned[m[0]:m[1]]
		target := strings.TrimSpace(cleaned[m[2]:m[3]])
		lower := strings.ToLower(target)
		if _, ok := idx[lower]; ok {
			continue
		}
		if cut := strings.LastIndex(lower, "/"); cut >= 0 {
			if _, ok := idx[lower[cut+1:]]; ok {
				continue
			}
		}
		violations = append(violations, Violation{
			File:     path,
			Line:     strings.Count(cleaned[:m[0]], "\n") + 1,
			Rule:     "broken-wikilink",
			Severity: SevError,
			Message:  fmt.Sprintf("Broken `[[wiki link]]`: `%s` does not match any page in the wiki.", target),
		})
		_ = full
	}
	return violations
}

// CheckFilename verifies a page's filename matches the project conventions.
func CheckFilename(path string, lc config.LintConfig) []Violation {
	name := filepath.Base(path)
	if slices.Contains(lc.AllowedUppercaseFilenames, name) {
		return nil
	}
	if datedFilenameRe.MatchString(name) {
		return nil
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if !filenameStemRe.MatchString(stem) {
		return []Violation{{
			File: path, Rule: "filename-casing", Severity: SevError,
			Message: fmt.Sprintf("Filename `%s` should be lowercase-hyphenated (e.g. `my-page.md`).", name),
			Autofix: true,
		}}
	}
	return nil
}

// CheckFrontmatter verifies a page's YAML frontmatter is present, parseable,
// and contains every required field.
func CheckFrontmatter(path, body string, lc config.LintConfig) []Violation {
	if !strings.HasPrefix(body, fmDelimiter) {
		return []Violation{{
			File: path, Line: 1, Rule: "missing-frontmatter", Severity: SevError,
			Message: "Page must start with YAML frontmatter (`---\\n...\\n---`).",
		}}
	}
	if !strings.Contains(body[len(fmDelimiter):], "\n---\n") {
		return []Violation{{
			File: path, Line: 1, Rule: "malformed-frontmatter", Severity: SevError,
			Message: "Frontmatter is not closed with `---`.",
		}}
	}
	fm := ExtractFrontmatter(body)
	if len(fm) == 0 {
		return []Violation{{
			File: path, Line: 1, Rule: "malformed-frontmatter", Severity: SevError,
			Message: "Frontmatter is empty or unparseable as YAML.",
		}}
	}
	var violations []Violation
	for _, field := range lc.RequiredFrontmatterFields {
		if _, ok := fm[field]; !ok {
			violations = append(violations, Violation{
				File: path, Line: 1, Rule: "missing-frontmatter-field", Severity: SevError,
				Message: fmt.Sprintf("Frontmatter missing required field `%s`.", field),
			})
		}
	}
	if statusVal, ok := fm["status"]; ok {
		statusStr := fmt.Sprintf("%v", statusVal)
		if !slices.Contains(lc.ValidStatusValues, statusStr) {
			violations = append(violations, Violation{
				File: path, Line: 1, Rule: "invalid-frontmatter-value", Severity: SevError,
				Message: fmt.Sprintf("Frontmatter `status: %s` is not in %v.", statusStr, lc.ValidStatusValues),
			})
		}
	}
	return violations
}

// RunAll runs every rule against the given file path.
func RunAll(path, wikiRoot string, lc config.LintConfig) ([]Violation, error) {
	body, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var v []Violation
	v = append(v, CheckTitle(path, body)...)
	v = append(v, CheckSummary(path, body)...)
	v = append(v, CheckSources(path, body)...)
	v = append(v, CheckSeeAlso(path, body)...)
	v = append(v, CheckWikilinks(path, body, wikiRoot)...)
	v = append(v, CheckFilename(path, lc)...)
	v = append(v, CheckFrontmatter(path, body, lc)...)
	return v, nil
}

// stripCodeRegions blanks out fenced and inline code blocks so the wikilink
// regex doesn't match patterns inside code samples. Line / column counts
// are preserved.
func stripCodeRegions(text string) string {
	var sb strings.Builder
	inFence := false
	for _, line := range strings.SplitAfter(text, "\n") {
		if fencedCodeOpenRe.MatchString(line) {
			inFence = !inFence
			if strings.HasSuffix(line, "\n") {
				sb.WriteByte('\n')
			}
			continue
		}
		if inFence {
			if strings.HasSuffix(line, "\n") {
				sb.WriteByte('\n')
			}
			continue
		}
		sb.WriteString(line)
	}
	noFences := sb.String()
	return inlineCodeRe.ReplaceAllStringFunc(noFences, func(match string) string {
		return strings.Repeat(" ", len(match))
	})
}

// buildPageIndex walks wikiRoot for *.md files and indexes them by
// lowercased stem and by the Obsidian display form ("my-page" → "my page").
func buildPageIndex(wikiRoot string) (map[string][]string, error) {
	idx := map[string][]string{}
	err := filepath.WalkDir(wikiRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == "_lint" {
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") && path != wikiRoot {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(name) != ".md" {
			return nil
		}
		stem := strings.TrimSuffix(name, ".md")
		lower := strings.ToLower(stem)
		idx[lower] = append(idx[lower], path)
		idx[strings.ReplaceAll(lower, "-", " ")] = append(idx[strings.ReplaceAll(lower, "-", " ")], path)
		return nil
	})
	return idx, err
}

// extractTitle returns the first `# Heading` line content, or "" if none.
func extractTitle(body string) string {
	if m := headingTitleRe.FindStringSubmatch(body); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
