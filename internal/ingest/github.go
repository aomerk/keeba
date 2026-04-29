package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PR is one merged pull request fetched via the gh CLI.
type PR struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	MergedAt time.Time `json:"mergedAt"`
	Labels   []ghLabel `json:"labels"`
	URL      string    `json:"url"`
	Author   ghAuthor  `json:"author"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

// reBodyDecisionHeading matches "## Decision", "## Trade-offs", "## ADR",
// "## Why this matters", etc — section headings that signal the PR IS an
// ADR, not just mentions one. Body-keyword matching was deliberately
// removed in v0.3 because prose mentions caused false positives; section
// headings are structural and don't have that risk.
var (
	reBodyDecisionHeading = regexp.MustCompile(`(?im)^#{2,4}\s+(decision|rationale|trade[- ]?offs?|architecture|adr|why this matters|why we chose)\b`)
	reBodyIncidentHeading = regexp.MustCompile(`(?im)^#{2,4}\s+(incident|outage|post[- ]?mortem|rca|what happened|root cause|hotfix)\b`)
	reBodyBreakingHeading = regexp.MustCompile(`(?im)^#{2,4}\s+(breaking|migration|backwards[- ]incompat)`)
)

// classifyPR returns the durable signal class for a PR. Combines:
//   - PR labels (highest signal, lowest noise — human-curated)
//   - Title-prefix Conventional Commits markers (BREAKING:, feat!:)
//   - Title keywords (incident / architecture / etc.)
//   - Body section headings (## Decision / ## Trade-offs / ## What happened)
//   - Major-version dep bump in title
//
// Body section headings are the v0.4 addition — they catch the common
// case where a PR with a plain "feat:" title still has a structured
// decision write-up in the body, which is the most useful signal to
// extract from a real PR history.
func classifyPR(p PR) Class {
	if hasLabel(p.Labels, "breaking", "breaking-change") {
		return ClassBreaking
	}
	if hasLabel(p.Labels, "incident", "post-mortem", "postmortem", "rca") {
		return ClassIncident
	}
	if hasLabel(p.Labels, "architecture", "adr", "decision", "design") {
		return ClassDecision
	}
	if reBreakingFooter.MatchString(p.Title+"\n"+p.Body) || reBreakingBang.MatchString(p.Title) {
		return ClassBreaking
	}
	if reIncident.MatchString(p.Title) || reBodyIncidentHeading.MatchString(p.Body) {
		return ClassIncident
	}
	if reDecision.MatchString(p.Title) || reBodyDecisionHeading.MatchString(p.Body) {
		return ClassDecision
	}
	if reBodyBreakingHeading.MatchString(p.Body) {
		return ClassBreaking
	}
	if m := reDependency.FindStringSubmatch(p.Title); len(m) == 3 && majorBumped(m[1], m[2]) {
		return ClassDependency
	}
	return ClassNone
}

func hasLabel(ls []ghLabel, want ...string) bool {
	for _, l := range ls {
		ln := strings.ToLower(l.Name)
		for _, w := range want {
			if ln == w {
				return true
			}
		}
	}
	return false
}

// GitHubResult summarizes one ingest run.
type GitHubResult struct {
	Imported []string // wiki-relative slugs that landed (created or appended)
	Skipped  []PR     // PRs already present in the wiki (idempotency)
	Noise    []PR     // PRs that did not match any heuristic
}

// fetchPRs shells out to `gh pr list` with the right --json fields. Limit
// caps the page size; gh's default is 30, max 1000.
func fetchPRs(repo, since string, limit int) ([]PR, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("`gh` CLI not on PATH; install GitHub CLI first: %w", err)
	}
	args := []string{
		"pr", "list",
		"-R", repo,
		"--state", "merged",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "number,title,body,mergedAt,labels,url,author",
	}
	out, err := exec.Command("gh", args...).Output() //nolint:gosec // repo is user input by design
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	var prs []PR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	if since == "" {
		return prs, nil
	}
	cutoff, err := parseSince(since)
	if err != nil {
		return nil, err
	}
	out2 := prs[:0]
	for _, p := range prs {
		if p.MergedAt.Before(cutoff) {
			continue
		}
		out2 = append(out2, p)
	}
	return out2, nil
}

// parseSince accepts a duration spec like "30d", "7.days.ago" (git style),
// "168h", or an RFC3339 timestamp. Always interpreted as "PRs merged on or
// after this point".
func parseSince(s string) (time.Time, error) {
	now := time.Now().UTC()
	if s == "" {
		return now.AddDate(0, 0, -7), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// "Nd" — N days
	if d, ok := parseDays(s); ok {
		return now.AddDate(0, 0, -d), nil
	}
	// Git-style "N.days.ago" — already covered by parseDays after stripping
	if d, ok := parseGitDays(s); ok {
		return now.AddDate(0, 0, -d), nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("can't parse --since %q (try 30d, 168h, 7.days.ago, or RFC3339)", s)
}

func parseDays(s string) (int, bool) {
	if !strings.HasSuffix(s, "d") {
		return 0, false
	}
	n := s[:len(s)-1]
	v, err := jsonAtoi(n)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseGitDays(s string) (int, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return 0, false
	}
	if !strings.Contains(parts[1], "day") {
		return 0, false
	}
	v, err := jsonAtoi(parts[0])
	if err != nil {
		return 0, false
	}
	return v, true
}

// jsonAtoi is strconv.Atoi without importing strconv just for one helper.
func jsonAtoi(s string) (int, error) {
	var v int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		v = v*10 + int(r-'0')
	}
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}
	return v, nil
}

// GitHub runs the GitHub-PR ingest. dryRun returns the planned actions
// without touching disk. repo is "owner/name".
func GitHub(wikiRoot, repo, since string, limit int, dryRun bool) (GitHubResult, error) {
	res := GitHubResult{}
	prs, err := fetchPRs(repo, since, limit)
	if err != nil {
		return res, err
	}
	for _, p := range prs {
		class := classifyPR(p)
		if class == ClassNone {
			res.Noise = append(res.Noise, p)
			continue
		}
		// Idempotency: if a page already exists with the matching pr_number
		// in its frontmatter, skip.
		if exists, slug := findExistingPRPage(wikiRoot, p.Number); exists {
			res.Skipped = append(res.Skipped, p)
			_ = slug
			continue
		}
		switch class {
		case ClassBreaking, ClassDependency:
			block := prAppendBlock(p, class)
			if !dryRun {
				if err := appendToLog(wikiRoot, []string{block}); err != nil {
					return res, err
				}
			}
			res.Imported = append(res.Imported, "log.md")
		case ClassIncident:
			slug := slugify(p.MergedAt.UTC().Format("2006-01-02") + "-pr-" +
				fmt.Sprintf("%d", p.Number) + "-" + firstWords(p.Title, 5))
			path := "investigations/" + slug + ".md"
			if !dryRun {
				if err := writePRPage(wikiRoot, path, prIncidentTemplate(p)); err != nil {
					return res, err
				}
			}
			res.Imported = append(res.Imported, path)
		case ClassDecision:
			slug := slugify("pr-" + fmt.Sprintf("%d", p.Number) + "-" + firstWords(p.Title, 6))
			path := "decisions/" + slug + ".md"
			if !dryRun {
				if err := writePRPage(wikiRoot, path, prDecisionTemplate(p)); err != nil {
					return res, err
				}
			}
			res.Imported = append(res.Imported, path)
		}
	}
	return res, nil
}

// findExistingPRPage walks decisions/ and investigations/ looking for a
// page whose frontmatter declares `pr_number: <n>`. Used to keep ingest
// idempotent across re-runs.
func findExistingPRPage(wikiRoot string, n int) (bool, string) {
	dirs := []string{"decisions", "investigations"}
	want := fmt.Sprintf("pr_number: %d", n)
	for _, d := range dirs {
		entries, err := os.ReadDir(filepath.Join(wikiRoot, d))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(wikiRoot, d, e.Name())) //nolint:gosec
			if err != nil {
				continue
			}
			if bytesContainsLine(b, want) {
				return true, filepath.Join(d, e.Name())
			}
		}
	}
	return false, ""
}

func bytesContainsLine(b []byte, line string) bool {
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

func writePRPage(wikiRoot, rel, body string) error {
	full := filepath.Join(wikiRoot, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(body), 0o644)
}

func prAppendBlock(p PR, class Class) string {
	return fmt.Sprintf(
		"## [%s] %s: pr#%d %s\n\n- author: @%s\n- url: %s\n- title: %s\n",
		p.MergedAt.UTC().Format("2006-01-02"),
		class,
		p.Number,
		trim(p.Title, 60),
		p.Author.Login,
		p.URL,
		p.Title,
	)
}

func prIncidentTemplate(p PR) string {
	return fmt.Sprintf(`---
tags: [incident, github-ingest]
last_verified: %s
status: current
pr_number: %d
---

# %s

> Auto-imported from PR #%d (%s). Replace with a real post-mortem when ready.

## What happened

%s

## Sources

- pr: %s
- merged: %s
- author: @%s

## See Also

- [[log]]
`,
		p.MergedAt.UTC().Format("2006-01-02"),
		p.Number,
		trim(p.Title, 80),
		p.Number, p.MergedAt.UTC().Format("2006-01-02"),
		ifEmpty(p.Body, "(no PR body)"),
		p.URL,
		p.MergedAt.UTC().Format(time.RFC3339),
		p.Author.Login,
	)
}

func prDecisionTemplate(p PR) string {
	return fmt.Sprintf(`---
tags: [decision, github-ingest]
last_verified: %s
status: current
pr_number: %d
---

# %s

> Auto-imported from PR #%d. Edit to match the real ADR shape.

## Context

%s

## Sources

- pr: %s
- merged: %s
- author: @%s

## See Also

- [[log]]
`,
		p.MergedAt.UTC().Format("2006-01-02"),
		p.Number,
		trim(p.Title, 80),
		p.Number,
		ifEmpty(p.Body, "(no PR body)"),
		p.URL,
		p.MergedAt.UTC().Format(time.RFC3339),
		p.Author.Login,
	)
}
