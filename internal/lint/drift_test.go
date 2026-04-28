package lint

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

func driftCfg() config.DriftConfig {
	return config.DriftConfig{
		RepoPrefixes:     []string{"my-app/", "my-infra/"},
		SkipPathPrefixes: []string{"graphify-out/", "wiki/", ".keeba/"},
		GigarepoRoot:     "..",
	}
}

func gigarepoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	makeLines := func(n int) string {
		var sb strings.Builder
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&sb, "line%d\n", i)
		}
		return sb.String()
	}
	writeFile(t, filepath.Join(root, "my-app", "cmd", "foo.go"), makeLines(400))
	writeFile(t, filepath.Join(root, "my-app", "CLAUDE.md"), "# App\n")
	writeFile(t, filepath.Join(root, "my-infra", "main.tf"), makeLines(50))
	writeFile(t, filepath.Join(root, "wiki", "concepts", "alpha.md"), "# Alpha\n")
	return root
}

func TestExtractCitations(t *testing.T) {
	dc := driftCfg()
	tests := []struct {
		name      string
		body      string
		wantPaths []string
	}{
		{
			"with line",
			"see `my-app/cmd/foo.go:113` for the loader.",
			[]string{"my-app/cmd/foo.go"},
		},
		{
			"no line",
			"see `my-app/CLAUDE.md` for context",
			[]string{"my-app/CLAUDE.md"},
		},
		{
			"in bullet list",
			"## Sources\n\n- `my-app/cmd/foo.go` — entry\n- `my-infra/main.tf:10` — IaC\n",
			[]string{"my-app/cmd/foo.go", "my-infra/main.tf"},
		},
		{
			"skip urls",
			"see https://github.com/foo/my-app/blob/main/foo.go for details",
			nil,
		},
		{
			"skip wikilinks",
			"see [[my-app]] for the page",
			nil,
		},
		{
			"skip configured prefix",
			"the report at `wiki/concepts/parity.md` is internal",
			nil,
		},
		{
			"unknown repo ignored",
			"this is just `some/random/path.go` not a real citation",
			nil,
		},
		{
			"fenced code ignored",
			"```\nsee `my-app/cmd/foo.go`\n```\n",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCitations(tt.body, "page.md", dc)
			gotPaths := make([]string, 0, len(got))
			for _, c := range got {
				gotPaths = append(gotPaths, c.RepoPath)
			}
			if len(gotPaths) != len(tt.wantPaths) {
				t.Fatalf("got %v, want %v", gotPaths, tt.wantPaths)
			}
			for i := range gotPaths {
				if gotPaths[i] != tt.wantPaths[i] {
					t.Fatalf("got %v, want %v", gotPaths, tt.wantPaths)
				}
			}
		})
	}
}

func TestExtractCitationWithLineRange(t *testing.T) {
	dc := driftCfg()
	got := ExtractCitations("the walk lives at `my-app/cmd/foo.go:113-129`", "page.md", dc)
	if len(got) != 1 || got[0].Line != 113 || got[0].LineEnd != 129 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractCitationsEmptyPrefixes(t *testing.T) {
	dc := config.DriftConfig{RepoPrefixes: nil}
	got := ExtractCitations("see `my-app/cmd/foo.go` even though it looks like one", "p.md", dc)
	if len(got) != 0 {
		t.Fatalf("expected no citations with empty prefixes, got %v", got)
	}
}

func TestVerify(t *testing.T) {
	root := gigarepoFixture(t)
	tests := []struct {
		name string
		c    Citation
		rule string
		sev  Severity
		ok   bool
	}{
		{
			name: "existing no line",
			c:    Citation{RepoPath: "my-app/CLAUDE.md", SourceFile: "p.md"},
			ok:   true,
		},
		{
			name: "existing in bounds",
			c:    Citation{RepoPath: "my-app/cmd/foo.go", Line: 113, SourceFile: "p.md"},
			ok:   true,
		},
		{
			name: "out of bounds",
			c:    Citation{RepoPath: "my-app/cmd/foo.go", Line: 999, SourceFile: "p.md"},
			rule: "citation-line-out-of-bounds",
			sev:  SevError,
		},
		{
			name: "missing file",
			c:    Citation{RepoPath: "my-app/cmd/never.go", SourceFile: "p.md"},
			rule: "citation-file-missing",
			sev:  SevError,
		},
		{
			name: "repo not cloned",
			c:    Citation{RepoPath: "other-repo/foo.go", SourceFile: "p.md"},
			rule: "citation-repo-not-cloned",
			sev:  SevWarning,
		},
		{
			name: "range in bounds",
			c:    Citation{RepoPath: "my-infra/main.tf", Line: 10, LineEnd: 20, SourceFile: "p.md"},
			ok:   true,
		},
		{
			name: "range partially oob",
			c:    Citation{RepoPath: "my-infra/main.tf", Line: 40, LineEnd: 100, SourceFile: "p.md"},
			rule: "citation-line-out-of-bounds",
			sev:  SevError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Verify(tt.c, root)
			if tt.ok {
				if len(got) != 0 {
					t.Fatalf("expected clean, got %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Rule != tt.rule || got[0].Severity != tt.sev {
				t.Fatalf("got %+v, want rule=%s sev=%s", got, tt.rule, tt.sev)
			}
		})
	}
}

func TestCheckPageEndToEnd(t *testing.T) {
	root := gigarepoFixture(t)
	dc := driftCfg()
	page := writeFile(t, filepath.Join(root, "wiki", "concepts", "test.md"), `# Test

## Sources

- `+"`my-app/cmd/foo.go:113`"+`
- `+"`my-app/CLAUDE.md`"+`
`)
	v, err := CheckPage(page, filepath.Join(root, "wiki"), dc)
	if err != nil {
		t.Fatalf("CheckPage: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("expected clean, got %+v", v)
	}

	bad := writeFile(t, filepath.Join(root, "wiki", "concepts", "bad.md"), `# Bad

## Sources

- `+"`my-app/cmd/foo.go:9999`"+`
- `+"`my-app/cmd/never.go`"+`
`)
	v, err = CheckPage(bad, filepath.Join(root, "wiki"), dc)
	if err != nil {
		t.Fatalf("CheckPage: %v", err)
	}
	rulesSeen := map[string]bool{}
	for _, x := range v {
		rulesSeen[x.Rule] = true
	}
	if !rulesSeen["citation-line-out-of-bounds"] || !rulesSeen["citation-file-missing"] {
		t.Fatalf("expected both rules, got %+v", v)
	}
}
