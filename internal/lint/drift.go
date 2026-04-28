package lint

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/aomerk/keeba/internal/config"
)

// Citation is a single backtick-wrapped repo path reference found in a page.
type Citation struct {
	SourceFile   string // wiki page that contains the citation
	LineInSource int
	RepoPath     string // e.g. "my-app/cmd/foo.go"
	Line         int    // 0 == no line specified
	LineEnd      int    // 0 == not a range
}

// BuildCiteRegex returns the citation matcher for the given repo prefixes,
// or nil when no prefixes are configured (callers treat nil as "skip").
func BuildCiteRegex(prefixes []string) *regexp.Regexp {
	if len(prefixes) == 0 {
		return nil
	}
	escaped := make([]string, len(prefixes))
	for i, p := range prefixes {
		escaped[i] = regexp.QuoteMeta(p)
	}
	pattern := "`((?:" + strings.Join(escaped, "|") + `)[A-Za-z0-9_./-]+?)(?::(\d+)(?:-(\d+))?)?` + "`"
	return regexp.MustCompile(pattern)
}

// stripFencesOnly preserves inline backticks (which carry citations) and
// blanks out fenced-code blocks so their contents don't produce false
// positives.
func stripFencesOnly(text string) string {
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
	return sb.String()
}

// ExtractCitations finds all repo-prefixed backtick citations in text.
func ExtractCitations(text, sourceFile string, dc config.DriftConfig) []Citation {
	if len(dc.RepoPrefixes) == 0 {
		return nil
	}
	cleaned := stripFencesOnly(text)
	re := BuildCiteRegex(dc.RepoPrefixes)
	if re == nil {
		return nil
	}
	var out []Citation
	for _, m := range re.FindAllStringSubmatchIndex(cleaned, -1) {
		path := cleaned[m[2]:m[3]]
		skip := false
		for _, p := range dc.SkipPathPrefixes {
			if strings.HasPrefix(path, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		var line, lineEnd int
		if m[4] != -1 {
			line, _ = strconv.Atoi(cleaned[m[4]:m[5]])
		}
		if m[6] != -1 {
			lineEnd, _ = strconv.Atoi(cleaned[m[6]:m[7]])
		}
		out = append(out, Citation{
			SourceFile:   sourceFile,
			LineInSource: strings.Count(cleaned[:m[0]], "\n") + 1,
			RepoPath:     path,
			Line:         line,
			LineEnd:      lineEnd,
		})
	}
	return out
}

// Verify checks one citation against the gigarepo on disk.
func Verify(c Citation, gigarepoRoot string) []Violation {
	target := filepath.Join(gigarepoRoot, c.RepoPath)
	repoName, _, _ := strings.Cut(c.RepoPath, "/")
	repoRoot := filepath.Join(gigarepoRoot, repoName)

	if st, err := os.Stat(repoRoot); err != nil || !st.IsDir() {
		return []Violation{{
			File: c.SourceFile, Line: c.LineInSource,
			Rule: "citation-repo-not-cloned", Severity: SevWarning,
			Message: fmt.Sprintf(
				"Citation references repo `%s` which is not present at `%s/%s`. Skipping verification.",
				repoName, gigarepoRoot, repoName),
		}}
	}
	if _, err := os.Stat(target); err != nil {
		return []Violation{{
			File: c.SourceFile, Line: c.LineInSource,
			Rule: "citation-file-missing", Severity: SevError,
			Message: fmt.Sprintf("Citation `%s` does not exist on disk.", c.RepoPath),
		}}
	}
	if c.Line == 0 {
		return nil
	}
	count, err := lineCount(target)
	if err != nil {
		return []Violation{{
			File: c.SourceFile, Line: c.LineInSource,
			Rule: "citation-file-unreadable", Severity: SevWarning,
			Message: fmt.Sprintf("Could not read `%s`: %v", c.RepoPath, err),
		}}
	}
	upper := c.Line
	if c.LineEnd != 0 {
		upper = c.LineEnd
	}
	if upper > count {
		suffix := ""
		if c.LineEnd != 0 {
			suffix = fmt.Sprintf("-%d", c.LineEnd)
		}
		return []Violation{{
			File: c.SourceFile, Line: c.LineInSource,
			Rule: "citation-line-out-of-bounds", Severity: SevError,
			Message: fmt.Sprintf(
				"Citation `%s:%d%s` references line %d but the file has only %d lines.",
				c.RepoPath, c.Line, suffix, upper, count),
		}}
	}
	return nil
}

// CheckPage runs drift on every citation in a single wiki page.
func CheckPage(page, wikiRoot string, dc config.DriftConfig) ([]Violation, error) {
	body, err := readFile(page)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", page, err)
	}
	body = StripFrontmatter(body)
	cites := ExtractCitations(body, page, dc)

	gigarepo := dc.GigarepoRoot
	if !filepath.IsAbs(gigarepo) {
		gigarepo = filepath.Clean(filepath.Join(wikiRoot, gigarepo))
	}

	var out []Violation
	for _, c := range cites {
		out = append(out, Verify(c, gigarepo)...)
	}
	return out, nil
}

func lineCount(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for s.Scan() {
		n++
	}
	return n, s.Err()
}
