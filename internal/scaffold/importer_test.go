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

// TestImportPageWithBodyTitleProducesLintCleanOutput pins the v0.3.0-alpha
// regression: when the source had its own `# Title`, the wrapper produced
// `> summary\n# Title` (summary before title), which fails the lint title
// check. The fix must always emit `# Title\n> summary\n…body`.
func TestImportPageWithBodyTitleProducesLintCleanOutput(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"),
		"# my-app\n\nA small app that does X. It uses Y for Z.\n\n## Architecture\n\nfoo.\n")
	if _, err := ImportFromRepo(wiki, repo, "my-app"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	titleIdx := strings.Index(got, "# my-app")
	summaryIdx := strings.Index(got, "> ")
	if titleIdx == -1 || summaryIdx == -1 {
		t.Fatalf("missing title or summary marker:\n%s", got)
	}
	if titleIdx > summaryIdx {
		t.Fatalf("title must precede summary; got summary at %d, title at %d", summaryIdx, titleIdx)
	}
	// Ensure the title appears exactly once (no duplication).
	if strings.Count(got, "# my-app\n") != 1 {
		t.Fatalf("title appears %d times; want 1\n%s", strings.Count(got, "# my-app\n"), got)
	}
}

func TestImportWalksSingularDocDir(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "doc", "layernorm.md"), "# layernorm\n\n> note.\n")
	if _, err := ImportFromRepo(wiki, repo, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wiki, "concepts", "doc-layernorm.md")); err != nil {
		t.Fatalf("expected doc/ to be walked: %v", err)
	}
}

func TestImportWalksNestedReadmes(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "scripts", "README.md"), "# scripts\n\n> helpers.\n")
	if _, err := ImportFromRepo(wiki, repo, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wiki, "concepts", "scripts-readme.md")); err != nil {
		t.Fatalf("expected scripts/README.md to be imported: %v", err)
	}
}

// ---- sync ----

func TestSyncRefreshesPristinePages(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "# v1\n\n> first version of the readme.\n\n## Sources\n\n## See Also\n")
	if _, err := ImportFromRepo(wiki, repo, "app"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if !strings.Contains(string(first), "# v1") {
		t.Fatalf("first import missing # v1\n%s", first)
	}

	// Source updates.
	writeFile(t, filepath.Join(repo, "README.md"), "# v2\n\n> second version with new content.\n\n## Sources\n\n## See Also\n")
	res, err := SyncFromRepo(wiki, repo, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 1 || res.Imported[0] != "concepts/readme.md" {
		t.Fatalf("expected sync to refresh readme.md, got %+v", res)
	}
	got, _ := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if !strings.Contains(string(got), "# v2") {
		t.Fatalf("sync did not pick up v2 source:\n%s", got)
	}
}

func TestSyncPreservesEditedPages(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "# original\n\n> first.\n\n## Sources\n\n## See Also\n")
	if _, err := ImportFromRepo(wiki, repo, "app"); err != nil {
		t.Fatal(err)
	}
	// User edits the page.
	original, _ := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	edited := strings.Replace(string(original), "# original", "# Heavily edited by human", 1)
	if err := os.WriteFile(filepath.Join(wiki, "concepts", "readme.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// Source updates.
	writeFile(t, filepath.Join(repo, "README.md"), "# upstream-changed\n\n> second.\n\n## Sources\n\n## See Also\n")
	res, err := SyncFromRepo(wiki, repo, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Edited) != 1 || res.Edited[0] != "concepts/readme.md" {
		t.Fatalf("expected sync to mark readme.md as edited, got %+v", res)
	}
	got, _ := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if !strings.Contains(string(got), "# Heavily edited by human") {
		t.Fatalf("sync clobbered the user's edits:\n%s", got)
	}
}

func TestSyncImportsNewSourceFiles(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "# A\n\n> A.\n")
	if _, err := ImportFromRepo(wiki, repo, "app"); err != nil {
		t.Fatal(err)
	}

	// New file appears in source.
	writeFile(t, filepath.Join(repo, "ARCHITECTURE.md"), "# Arch\n\n> Arch.\n")
	res, err := SyncFromRepo(wiki, repo, "app")
	if err != nil {
		t.Fatal(err)
	}
	gotSlugs := map[string]bool{}
	for _, s := range res.Imported {
		gotSlugs[s] = true
	}
	if !gotSlugs["concepts/architecture.md"] {
		t.Fatalf("expected new architecture.md to be imported on sync, got %+v", res)
	}
}

func TestSyncSkipsManualPages(t *testing.T) {
	wiki := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "# from source\n\n> ...\n")
	// Manual page (no keeba_pristine_hash) collides with the slug.
	manualBody := "---\ntags: [manual]\nlast_verified: 2026-04-28\nstatus: current\n---\n\n# Hand-curated\n\n> Manual.\n"
	writeFile(t, filepath.Join(wiki, "concepts", "readme.md"), manualBody)

	res, err := SyncFromRepo(wiki, repo, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Edited) != 1 {
		t.Fatalf("manual page should be marked Edited, got %+v", res)
	}
	got, _ := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if string(got) != manualBody {
		t.Fatalf("manual page was clobbered:\n%s", got)
	}
}
