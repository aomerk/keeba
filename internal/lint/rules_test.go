package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

func writeFile(t *testing.T, path, contents string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

const validFM = "---\ntags: [test]\nlast_verified: 2026-04-27\nstatus: current\n---\n\n"

func TestCheckTitle(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantHit bool
	}{
		{"valid", "# My Page\n\n> s\n", false},
		{"missing", "Some content without a title\n", true},
		{"wrong level", "## Subhead first\n", true},
		{"valid after frontmatter", validFM + "# My Page\n\n> s\n", false},
		{"empty file", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckTitle("p.md", tt.body)
			if (len(got) > 0) != tt.wantHit {
				t.Fatalf("got %v, wantHit %v", got, tt.wantHit)
			}
			if tt.wantHit && got[0].Rule != "missing-title" {
				t.Fatalf("rule: %s", got[0].Rule)
			}
		})
	}
}

func TestCheckSummary(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantHit bool
	}{
		{"valid", "# Title\n\n> One-line summary.\n", false},
		{"missing", "# Title\n\nNo summary line.\n", true},
		{"too late", "# Title\n\n" + strings.Repeat("filler\n", 10) + "> Late summary\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckSummary("p.md", tt.body)
			if (len(got) > 0) != tt.wantHit {
				t.Fatalf("got %v, wantHit %v", got, tt.wantHit)
			}
		})
	}
}

func TestCheckSourcesAndSeeAlso(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantSources bool
		wantSeeAlso bool
	}{
		{"both present", "# T\n\n> s\n\n## Sources\n\n## See Also\n", false, false},
		{"sources missing", "# T\n\n> s\n\n## See Also\n", true, false},
		{"see also missing", "# T\n\n> s\n\n## Sources\n", false, true},
		{"both missing", "# T\n\n> s\n", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotS := CheckSources("p.md", tt.body)
			gotSA := CheckSeeAlso("p.md", tt.body)
			if (len(gotS) > 0) != tt.wantSources {
				t.Fatalf("Sources mismatch: %v", gotS)
			}
			if (len(gotSA) > 0) != tt.wantSeeAlso {
				t.Fatalf("SeeAlso mismatch: %v", gotSA)
			}
		})
	}
}

func TestCheckWikilinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "alpha.md"), validFM+"# Alpha\n\n> s\n\n## Sources\n\n## See Also\n")
	writeFile(t, filepath.Join(root, "concepts", "beta.md"), validFM+"# Beta\n\n> s\n\n## Sources\n\n## See Also\n")

	tests := []struct {
		name    string
		body    string
		wantHit int
	}{
		{"valid", "# G\n\n## See Also\n- [[alpha]]\n- [[beta]]\n", 0},
		{"alias", "# G\n\n## See Also\n- [[alpha|Custom Display]]\n", 0},
		{"case insensitive", "# G\n\n## See Also\n- [[Alpha]]\n", 0},
		{"path-prefixed", "# G\n\n## See Also\n- [[concepts/alpha]]\n", 0},
		{"broken", "# G\n\n## See Also\n- [[nonexistent-page]]\n", 1},
		{"in inline code", "# G\n\nUse `[[Nonexistent]]` syntax.\n\n## See Also\n- [[alpha]]\n", 0},
		{"in fenced code", "# G\n\n```\n[[Nonexistent]]\n```\n\n## See Also\n- [[alpha]]\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckWikilinks(filepath.Join(root, "page.md"), tt.body, root)
			if len(got) != tt.wantHit {
				t.Fatalf("hits %d, want %d (%v)", len(got), tt.wantHit, got)
			}
		})
	}
}

func TestCheckFilename(t *testing.T) {
	lc := config.Defaults().Lint
	tests := []struct {
		name    string
		path    string
		wantHit bool
	}{
		{"valid", "good-name.md", false},
		{"uppercase", "BadName.md", true},
		{"underscored", "bad_name.md", true},
		{"dated allowed", "2026-04-27.md", false},
		{"SCHEMA allowed", "SCHEMA.md", false},
		{"README allowed", "README.md", false},
		{"QUERY_PATTERNS allowed", "QUERY_PATTERNS.md", false},
		{"leading hyphen rejected", "-bad.md", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckFilename(tt.path, lc)
			if (len(got) > 0) != tt.wantHit {
				t.Fatalf("got %v, wantHit %v", got, tt.wantHit)
			}
		})
	}
}

func TestCheckFrontmatter(t *testing.T) {
	lc := config.Defaults().Lint
	const body = "# T\n\n> s\n\n## Sources\n\n## See Also\n"
	tests := []struct {
		name      string
		input     string
		wantRules []string
	}{
		{
			"valid",
			"---\ntags: [foo]\nlast_verified: 2026-04-27\nstatus: current\n---\n\n" + body,
			nil,
		},
		{
			"missing entirely",
			body,
			[]string{"missing-frontmatter"},
		},
		{
			"missing tags",
			"---\nlast_verified: 2026-04-27\nstatus: current\n---\n\n" + body,
			[]string{"missing-frontmatter-field"},
		},
		{
			"missing last_verified",
			"---\ntags: [foo]\nstatus: current\n---\n\n" + body,
			[]string{"missing-frontmatter-field"},
		},
		{
			"missing status",
			"---\ntags: [foo]\nlast_verified: 2026-04-27\n---\n\n" + body,
			[]string{"missing-frontmatter-field"},
		},
		{
			"invalid status value",
			"---\ntags: [foo]\nlast_verified: 2026-04-27\nstatus: yolo\n---\n\n" + body,
			[]string{"invalid-frontmatter-value"},
		},
		{
			"malformed yaml",
			"---\nthis: is: not: valid\n---\n\n" + body,
			[]string{"malformed-frontmatter"},
		},
		{
			"empty frontmatter",
			"---\n---\n\n" + body,
			[]string{"malformed-frontmatter"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckFrontmatter("p.md", tt.input, lc)
			gotRules := map[string]bool{}
			for _, v := range got {
				gotRules[v.Rule] = true
			}
			for _, want := range tt.wantRules {
				if !gotRules[want] {
					t.Fatalf("expected %q in %v", want, got)
				}
			}
			if len(tt.wantRules) == 0 && len(got) != 0 {
				t.Fatalf("unexpected violations: %v", got)
			}
		})
	}
}

func TestRunAllAggregates(t *testing.T) {
	root := t.TempDir()
	page := writeFile(t, filepath.Join(root, "concepts", "BadName.md"), "no title or anything\n")
	v, err := RunAll(page, root, config.Defaults().Lint)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	got := map[string]bool{}
	for _, x := range v {
		got[x.Rule] = true
	}
	for _, want := range []string{"missing-title", "missing-summary", "missing-sources", "missing-see-also", "filename-casing", "missing-frontmatter"} {
		if !got[want] {
			t.Errorf("expected %q, got %v", want, v)
		}
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"# Hello\nbody\n", "Hello"},
		{"## Sub\n# Real\n", "Real"},
		{"no title\n", ""},
	}
	for _, tt := range tests {
		if got := extractTitle(tt.in); got != tt.want {
			t.Fatalf("extractTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
