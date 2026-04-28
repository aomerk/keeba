package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mkCommit(subj, body string) Commit {
	return Commit{
		SHA: "abcdef1234567890", Date: time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
		Subject: subj, Body: body,
	}
}

func TestClassifyBreaking(t *testing.T) {
	got := Classify(mkCommit("BREAKING: rename Foo to Bar", ""))
	if len(got) != 1 || got[0].Class != ClassBreaking {
		t.Fatalf("got %+v", got)
	}
	if got[0].AppendPath != "log.md" {
		t.Fatalf("append: %s", got[0].AppendPath)
	}
	if !strings.Contains(got[0].AppendBlock, "## [2026-04-28] breaking:") {
		t.Fatalf("block: %s", got[0].AppendBlock)
	}
}

func TestClassifyIncident(t *testing.T) {
	got := Classify(mkCommit("hotfix: payment service outage RCA", ""))
	if len(got) == 0 {
		t.Fatal("no classification")
	}
	found := false
	for _, a := range got {
		if a.Class == ClassIncident {
			found = true
			if !strings.HasPrefix(a.TargetPath, "investigations/2026-04-28-") {
				t.Errorf("target: %s", a.TargetPath)
			}
			for _, want := range []string{"---", "tags: [incident", "## Sources", "## See Also"} {
				if !strings.Contains(a.NewBody, want) {
					t.Errorf("body missing %q\n%s", want, a.NewBody)
				}
			}
		}
	}
	if !found {
		t.Fatal("no incident classification")
	}
}

func TestClassifyDecision(t *testing.T) {
	got := Classify(mkCommit("architecture: pick OpenSearch over Elastic", "tradeoff body"))
	if len(got) == 0 {
		t.Fatal("no classification")
	}
	found := false
	for _, a := range got {
		if a.Class == ClassDecision {
			found = true
			if !strings.HasPrefix(a.TargetPath, "decisions/") {
				t.Errorf("target: %s", a.TargetPath)
			}
		}
	}
	if !found {
		t.Fatal("no decision class")
	}
}

func TestClassifyDependency(t *testing.T) {
	cases := []struct {
		subj string
		hit  bool
	}{
		{"chore(deps): bump react from v17.0.2 to v18.2.0", true},
		{"chore: bump go from 1.21.0 to 1.22.3", false},    // not major
		{"chore(deps): bump foo from v1.0 to v1.5", false}, // not major
		{"deps: bump axios from 0.27 to 1.5", true},        // major (0 → 1)
	}
	for _, tt := range cases {
		t.Run(tt.subj, func(t *testing.T) {
			got := Classify(mkCommit(tt.subj, ""))
			gotHit := false
			for _, a := range got {
				if a.Class == ClassDependency {
					gotHit = true
				}
			}
			if gotHit != tt.hit {
				t.Fatalf("got=%v want=%v", gotHit, tt.hit)
			}
		})
	}
}

func TestClassifyMultiple(t *testing.T) {
	got := Classify(mkCommit("BREAKING: incident in payment processing", ""))
	classes := map[Class]bool{}
	for _, a := range got {
		classes[a.Class] = true
	}
	if !classes[ClassBreaking] || !classes[ClassIncident] {
		t.Fatalf("expected both, got %v", classes)
	}
}

func TestClassifyNoise(t *testing.T) {
	got := Classify(mkCommit("typo fix in README", ""))
	if len(got) != 0 {
		t.Fatalf("expected no class, got %+v", got)
	}
}

// TestClassifyDoesNotMatchKeywordsInBody pins the v0.3.0-alpha regression:
// a commit body that *describes* the heuristic ("classifies by BREAKING:,
// incident keywords, ADR markers") was wrongly flagged as breaking +
// incident + decision. Subject is the only signal-rich place; bodies dilute.
func TestClassifyDoesNotMatchKeywordsInBody(t *testing.T) {
	c := mkCommit(
		"feat: keeba init --from-repo, mcp install, ingest git --execute",
		`Three user-facing wins.
## keeba ingest git --execute
Walks git log. Classifies each commit by regex (BREAKING:, incident
keywords, ADR markers, major-version dep bumps) and writes log/investigations.
`,
	)
	got := Classify(c)
	if len(got) != 0 {
		t.Fatalf("expected no class for prose-mention of keywords; got %+v", got)
	}
}

// TestClassifyBreakingMatchesAtSubjectStart confirms the tightened regex
// still matches the canonical "BREAKING:" / "feat!:" prefix forms.
func TestClassifyBreakingMatchesAtSubjectStart(t *testing.T) {
	cases := []string{
		"BREAKING: rename Foo",
		"feat!: drop legacy API",
		"feat(api)!: remove /v1",
	}
	for _, subj := range cases {
		t.Run(subj, func(t *testing.T) {
			got := Classify(mkCommit(subj, ""))
			found := false
			for _, a := range got {
				if a.Class == ClassBreaking {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected breaking for %q, got %+v", subj, got)
			}
		})
	}
}

func TestIngestGitDryRunSkipsFilesystem(t *testing.T) {
	wiki := t.TempDir()
	if err := os.WriteFile(filepath.Join(wiki, "log.md"), []byte("# log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use a fake repo path; readCommits requires a .git dir, so we expect an
	// error here. The point is that we exercise the wiring path.
	_, err := Git(wiki, "/no/such/repo", "1.day.ago", true)
	if err == nil {
		t.Fatal("expected error on missing repo")
	}
}

func TestAppendToLogPreservesExisting(t *testing.T) {
	wiki := t.TempDir()
	logPath := filepath.Join(wiki, "log.md")
	if err := os.WriteFile(logPath, []byte("# Log\n\n> initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendToLog(wiki, []string{"## block A\n", "## block B\n"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(logPath)
	for _, want := range []string{"# Log", "> initial", "## block A", "## block B"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %q\n%s", want, got)
		}
	}
}

func TestSlugify(t *testing.T) {
	if got := slugify("Hello World!"); got != "hello-world" {
		t.Fatalf("got %q", got)
	}
	if got := slugify(""); got != "untitled" {
		t.Fatalf("got %q", got)
	}
}

func TestMajorBumped(t *testing.T) {
	if !majorBumped("v17.0.2", "v18.0.0") {
		t.Fatal("17→18 should be major")
	}
	if majorBumped("v1.0.0", "v1.5.0") {
		t.Fatal("minor shouldn't count")
	}
	if majorBumped("v1", "") {
		t.Fatal("empty should be false")
	}
}
