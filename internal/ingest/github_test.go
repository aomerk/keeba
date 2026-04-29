package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mkPR(title, body string, labels ...string) PR {
	ls := make([]ghLabel, len(labels))
	for i, n := range labels {
		ls[i] = ghLabel{Name: n}
	}
	return PR{
		Number:   42,
		Title:    title,
		Body:     body,
		Labels:   ls,
		MergedAt: time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
		URL:      "https://github.com/x/y/pull/42",
		Author:   ghAuthor{Login: "alice"},
	}
}

func TestClassifyPRByLabel(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   Class
	}{
		{"breaking label", []string{"breaking"}, ClassBreaking},
		{"incident label", []string{"post-mortem"}, ClassIncident},
		{"architecture label", []string{"architecture"}, ClassDecision},
		{"adr label", []string{"adr"}, ClassDecision},
		{"label case-insensitive", []string{"Architecture"}, ClassDecision},
		{"unrelated label", []string{"chore"}, ClassNone},
		{"no labels", nil, ClassNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyPR(mkPR("plain title", "", tt.labels...)); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestClassifyPRByTitle(t *testing.T) {
	tests := []struct {
		title string
		want  Class
	}{
		{"BREAKING: drop legacy API", ClassBreaking},
		{"feat!: rename Foo", ClassBreaking},
		{"feat(api)!: drop /v1", ClassBreaking},
		{"hotfix: payment service outage", ClassIncident},
		{"architecture: pick OpenSearch over Elastic", ClassDecision},
		{"chore(deps): bump react from v17.0.2 to v18.2.0", ClassDependency},
		{"feat: add new search field", ClassNone},
		{"docs: typo in README", ClassNone},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := classifyPR(mkPR(tt.title, "")); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestClassifyPRDoesNotMatchKeywordsInBody(t *testing.T) {
	// The body describes the heuristic — must not classify.
	p := mkPR(
		"feat: keeba init --from-repo, mcp install, ingest git --execute",
		"Three user-facing wins. classifies by BREAKING:, incident keywords, ADR markers, dep bumps.",
	)
	if got := classifyPR(p); got != ClassNone {
		t.Fatalf("expected ClassNone for prose-mention; got %s", got)
	}
}

func TestClassifyPRBySectionHeading(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Class
	}{
		{
			"## Decision heading",
			"# Summary\n\n## Decision\n\nWe picked X over Y because Z.\n",
			ClassDecision,
		},
		{
			"## Trade-offs heading",
			"# Summary\n\n## Trade-offs\n\nA vs B.\n",
			ClassDecision,
		},
		{
			"## What happened heading",
			"# Summary\n\n## What happened\n\nAuth broke at 2am.\n",
			ClassIncident,
		},
		{
			"## Breaking changes heading",
			"# Summary\n\n## Breaking changes\n\nX is gone.\n",
			ClassBreaking,
		},
		{
			"prose mention without heading",
			"This PR talks about decision making and architecture but doesn't have a heading for it.",
			ClassNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyPR(mkPR("plain title", tt.body)); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestClassifyPRLabelTrumpsTitle(t *testing.T) {
	// PR labeled architecture but title looks like noise → still decision.
	p := mkPR("docs: small update", "", "architecture")
	if got := classifyPR(p); got != ClassDecision {
		t.Fatalf("label should win, got %s", got)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		in    string
		check func(t *testing.T, got time.Time)
	}{
		{"7d", func(t *testing.T, got time.Time) {
			if d := now.Sub(got); d < 6*24*time.Hour || d > 8*24*time.Hour {
				t.Fatalf("7d out of range: %v", d)
			}
		}},
		{"30.days.ago", func(t *testing.T, got time.Time) {
			if d := now.Sub(got); d < 29*24*time.Hour || d > 31*24*time.Hour {
				t.Fatalf("30.days.ago out of range: %v", d)
			}
		}},
		{"168h", func(t *testing.T, got time.Time) {
			if d := now.Sub(got); d < 167*time.Hour || d > 169*time.Hour {
				t.Fatalf("168h out of range: %v", d)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSince(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, got)
		})
	}
}

func TestParseSinceInvalid(t *testing.T) {
	if _, err := parseSince("garbage"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPRTemplatesShape(t *testing.T) {
	p := mkPR("feat: thing", "Decided X over Y because Z.")
	dec := prDecisionTemplate(p)
	for _, want := range []string{
		"---", "tags: [decision, github-ingest]", "pr_number: 42",
		"# feat: thing", "## Context", "## Sources", "## See Also", "https://github.com/x/y/pull/42",
		"author: @alice",
	} {
		if !strings.Contains(dec, want) {
			t.Errorf("decision template missing %q\n%s", want, dec)
		}
	}

	inc := prIncidentTemplate(p)
	for _, want := range []string{"tags: [incident, github-ingest]", "## What happened", "(no PR body)" /* will be replaced */} {
		if want == "(no PR body)" {
			continue
		}
		if !strings.Contains(inc, want) {
			t.Errorf("incident template missing %q\n%s", want, inc)
		}
	}
}

func TestFindExistingPRPageDetectsByFrontmatter(t *testing.T) {
	wiki := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wiki, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntags: [decision]\nlast_verified: 2026-04-28\nstatus: current\npr_number: 7\n---\n\n# Title\n"
	if err := os.WriteFile(filepath.Join(wiki, "decisions", "x.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if exists, _ := findExistingPRPage(wiki, 7); !exists {
		t.Fatal("expected to find pr_number: 7 page")
	}
	if exists, _ := findExistingPRPage(wiki, 99); exists {
		t.Fatal("should not match pr_number: 99")
	}
}

func TestPRAppendBlockShape(t *testing.T) {
	p := mkPR("BREAKING: rename Foo", "")
	got := prAppendBlock(p, ClassBreaking)
	for _, want := range []string{"## [2026-04-28] breaking: pr#42", "@alice", "https://github.com/x/y/pull/42"} {
		if !strings.Contains(got, want) {
			t.Errorf("append block missing %q\n%s", want, got)
		}
	}
}
