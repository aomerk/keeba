// Package ingest holds the in-process executors for keeba's ingest agents.
//
// v0.3 ships the git executor — heuristic-only, no LLM, no API key. Walks
// `git log`, classifies commits by regex against the subject + body, and
// either appends to the wiki's log.md or creates pages under
// investigations/ and decisions/.
package ingest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Commit is one git log entry.
type Commit struct {
	SHA     string
	Date    time.Time
	Subject string
	Body    string
}

// Class is the kind of durable signal a commit carries.
type Class int

// Class values.
const (
	// ClassNone — commit didn't match any heuristic.
	ClassNone Class = iota
	// ClassBreaking — BREAKING: / BREAKING CHANGE: in subject or body.
	ClassBreaking
	// ClassIncident — incident / outage / RCA / post-mortem / hotfix keyword.
	ClassIncident
	// ClassDecision — architecture / decision / ADR / trade-off keyword.
	ClassDecision
	// ClassDependency — major-version dep bump in the subject.
	ClassDependency
)

func (c Class) String() string {
	switch c {
	case ClassBreaking:
		return "breaking"
	case ClassIncident:
		return "incident"
	case ClassDecision:
		return "decision"
	case ClassDependency:
		return "dependency"
	default:
		return "none"
	}
}

// Action describes how a classified commit should land in the wiki.
type Action struct {
	Class       Class
	Commit      Commit
	TargetPath  string // wiki-relative
	AppendPath  string // wiki-relative; empty if Action creates a new page
	NewBody     string // body for new pages; ignored for append actions
	AppendBlock string // block to append to AppendPath
}

var (
	reBreaking   = regexp.MustCompile(`(?m)\bBREAKING(?:\s+CHANGE)?:`)
	reIncident   = regexp.MustCompile(`(?i)\b(incident|outage|rca|post[- ]?mortem|hotfix)\b`)
	reDecision   = regexp.MustCompile(`(?i)\b(architecture|decision|adr|trade[- ]?off)\b`)
	reDependency = regexp.MustCompile(`(?i)\bbump\s+\S+\s+from\s+(\S+)\s+to\s+(\S+)`)
	reSlugSafe   = regexp.MustCompile(`[^a-z0-9-]+`)
)

// Classify returns zero or more Actions for one commit. Multiple actions are
// possible (a "BREAKING incident hotfix" commit logs once *and* spawns an
// investigation page).
func Classify(c Commit) []Action {
	var out []Action
	body := c.Subject + "\n" + c.Body
	if reBreaking.MatchString(body) {
		out = append(out, Action{
			Class: ClassBreaking, Commit: c,
			AppendPath: "log.md",
			AppendBlock: fmt.Sprintf(
				"## [%s] breaking: %s\n\n- commit: `%s`\n- subject: %s\n",
				c.Date.UTC().Format("2006-01-02"), trim(c.Subject, 60), c.SHA[:short(c.SHA, 7)], c.Subject),
		})
	}
	if reIncident.MatchString(body) {
		slug := slugify(c.Date.UTC().Format("2006-01-02") + "-" + firstWords(c.Subject, 5))
		out = append(out, Action{
			Class: ClassIncident, Commit: c,
			TargetPath: "investigations/" + slug + ".md",
			NewBody:    incidentTemplate(c),
		})
	}
	if reDecision.MatchString(body) {
		slug := slugify(firstWords(c.Subject, 6))
		out = append(out, Action{
			Class: ClassDecision, Commit: c,
			TargetPath: "decisions/" + slug + ".md",
			NewBody:    decisionTemplate(c),
		})
	}
	if m := reDependency.FindStringSubmatch(c.Subject); len(m) == 3 && majorBumped(m[1], m[2]) {
		out = append(out, Action{
			Class: ClassDependency, Commit: c,
			AppendPath: "log.md",
			AppendBlock: fmt.Sprintf(
				"## [%s] dep: %s\n\n- commit: `%s`\n- %s → %s\n",
				c.Date.UTC().Format("2006-01-02"), trim(c.Subject, 60), c.SHA[:short(c.SHA, 7)], m[1], m[2]),
		})
	}
	return out
}

// Git runs the heuristic ingest over a git repo. Returns one Action per
// write that was performed. dryRun returns the planned actions without
// touching disk.
func Git(wikiRoot, repoPath, since string, dryRun bool) ([]Action, error) {
	commits, err := readCommits(repoPath, since)
	if err != nil {
		return nil, err
	}
	var actions []Action
	for _, c := range commits {
		actions = append(actions, Classify(c)...)
	}
	if dryRun || len(actions) == 0 {
		return actions, nil
	}

	// Group log.md appends so we write them once.
	var logAppends []string
	performed := actions[:0]
	for _, a := range actions {
		switch {
		case a.AppendPath == "log.md":
			logAppends = append(logAppends, a.AppendBlock)
			performed = append(performed, a)
		case a.TargetPath != "":
			full := filepath.Join(wikiRoot, a.TargetPath)
			if _, err := os.Stat(full); err == nil {
				// Already exists; skip silently.
				continue
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return performed, err
			}
			if err := os.WriteFile(full, []byte(a.NewBody), 0o644); err != nil {
				return performed, err
			}
			performed = append(performed, a)
		}
	}
	if len(logAppends) > 0 {
		if err := appendToLog(wikiRoot, logAppends); err != nil {
			return performed, err
		}
	}
	return performed, nil
}

func appendToLog(wikiRoot string, blocks []string) error {
	target := filepath.Join(wikiRoot, "log.md")
	body, err := os.ReadFile(target) //nolint:gosec
	if err != nil {
		return fmt.Errorf("read log.md: %w (run keeba init first)", err)
	}
	out := strings.TrimRight(string(body), "\n") + "\n\n" + strings.Join(blocks, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(target, []byte(out), 0o644)
}

func readCommits(repoPath, since string) ([]Commit, error) {
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return nil, fmt.Errorf("%s is not a git repo (no .git dir)", repoPath)
	}
	if since == "" {
		since = "7.days.ago"
	}
	const sep = "<<<END>>>"
	out, err := exec.Command( //nolint:gosec // repoPath is a user-supplied directory by design
		"git", "-C", repoPath, "log", "--since="+since,
		"--pretty=format:%H|%aI|%s%n%b"+sep,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	chunks := strings.Split(string(out), sep)
	var commits []Commit
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		// First line: SHA|date|subject. Rest: body.
		nl := strings.Index(chunk, "\n")
		head := chunk
		body := ""
		if nl != -1 {
			head = chunk[:nl]
			body = strings.TrimSpace(chunk[nl+1:])
		}
		parts := strings.SplitN(head, "|", 3)
		if len(parts) != 3 {
			continue
		}
		t, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			continue
		}
		commits = append(commits, Commit{
			SHA: parts[0], Date: t, Subject: parts[2], Body: body,
		})
	}
	sort.Slice(commits, func(i, j int) bool { return commits[i].Date.Before(commits[j].Date) })
	return commits, nil
}

func incidentTemplate(c Commit) string {
	return fmt.Sprintf(`---
tags: [incident, git-ingest]
last_verified: %s
status: current
---

# %s

> Auto-imported from commit %s. Replace this with a real post-mortem.

## What happened

%s

## Sources

- commit: `+"`%s`"+`

## See Also

- [[log]]
`,
		c.Date.UTC().Format("2006-01-02"),
		trim(c.Subject, 80),
		c.SHA[:short(c.SHA, 7)],
		ifEmpty(c.Body, "(no commit body)"),
		c.SHA)
}

func decisionTemplate(c Commit) string {
	return fmt.Sprintf(`---
tags: [decision, git-ingest]
last_verified: %s
status: current
---

# %s

> Auto-imported from commit %s. Edit to match the real ADR shape.

## Context

%s

## Sources

- commit: `+"`%s`"+`

## See Also

- [[log]]
`,
		c.Date.UTC().Format("2006-01-02"),
		trim(c.Subject, 80),
		c.SHA[:short(c.SHA, 7)],
		ifEmpty(c.Body, "(no commit body)"),
		c.SHA)
}

// helpers

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func short(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}

func ifEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.Join(fields, " ")
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = reSlugSafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

func majorBumped(from, to string) bool {
	fmajor := majorOf(from)
	tmajor := majorOf(to)
	if fmajor == "" || tmajor == "" {
		return false
	}
	return fmajor != tmajor
}

func majorOf(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 2)
	return parts[0]
}
