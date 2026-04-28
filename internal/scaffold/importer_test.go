package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportFromRepoBasics(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "# my-app\n\n> A small app.\n\n## Architecture\n\nFoo.\n")
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "# CLAUDE notes\n\nstuff.\n")
	writeFile(t, filepath.Join(repo, "docs", "auth.md"), "# Auth\n\n> JWT.\n")
	writeFile(t, filepath.Join(repo, "docs", "deep", "x.md"), "# X\n")

	if err := os.MkdirAll(filepath.Join(wiki, "concepts"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := ImportFromRepo(wiki, repo, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"concepts/readme.md",
		"concepts/claude.md",
		"concepts/docs-auth.md",
		"concepts/docs-deep-x.md",
	}
	got := map[string]bool{}
	for _, s := range res.Imported {
		got[s] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing %s in %v", w, res.Imported)
		}
	}

	rd, err := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"---", "tags:", "last_verified:", "status: current", "## Sources", "## See Also", "# my-app"} {
		if !strings.Contains(string(rd), want) {
			t.Errorf("readme.md missing %q\n%s", want, rd)
		}
	}
}

func TestImportSkipsExistingFiles(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "# orig\n")
	writeFile(t, filepath.Join(wiki, "concepts", "readme.md"), "PRESERVED")
	if _, err := ImportFromRepo(wiki, repo, "x"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "PRESERVED" {
		t.Fatalf("existing file overwritten: %s", got)
	}
}

func TestImportStripsIncomingFrontmatter(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"),
		"---\nfoo: bar\n---\n\n# title\n\n> sum.\n")
	if _, err := ImportFromRepo(wiki, repo, "x"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	body := string(got)
	if strings.Contains(body, "foo: bar") {
		t.Fatalf("incoming frontmatter not stripped:\n%s", body)
	}
	// Must still satisfy keeba lint shape.
	for _, want := range []string{"tags:", "## Sources", "## See Also", "# title"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\n%s", want, body)
		}
	}
}

func TestImportSummaryFallback(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "# Title only\n")
	if _, err := ImportFromRepo(wiki, repo, "x"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if !strings.Contains(string(got), "> Imported from x") {
		t.Errorf("synthetic summary missing\n%s", got)
	}
}

func TestImportNonexistentRepoErrors(t *testing.T) {
	if _, err := ImportFromRepo(t.TempDir(), "/no/such/path/here", "x"); err == nil {
		t.Fatal("expected error")
	}
}
