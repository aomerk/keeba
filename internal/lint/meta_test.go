package lint

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

func TestPageRecordWithFrontmatter(t *testing.T) {
	root := t.TempDir()
	page := writeFile(t, filepath.Join(root, "concepts", "alpha.md"), `---
tags: [foo, bar]
last_verified: 2026-04-27
status: current
---

# Alpha

> summary
`)
	rec, err := PageRecordFor(page, root, config.Defaults().Drift)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Slug != "concepts/alpha" {
		t.Fatalf("slug: %q", rec.Slug)
	}
	if rec.Title != "Alpha" {
		t.Fatalf("title: %q", rec.Title)
	}
	if rec.Category != "concepts" {
		t.Fatalf("category: %q", rec.Category)
	}
	if !reflect.DeepEqual(rec.Tags, []string{"foo", "bar"}) {
		t.Fatalf("tags: %v", rec.Tags)
	}
	if rec.LastVerified != "2026-04-27" {
		t.Fatalf("last_verified: %q", rec.LastVerified)
	}
	if rec.Status != "current" {
		t.Fatalf("status: %q", rec.Status)
	}
}

func TestPageRecordNoFrontmatter(t *testing.T) {
	root := t.TempDir()
	page := writeFile(t, filepath.Join(root, "concepts", "beta.md"), "# Beta\n\n> s\n")
	rec, err := PageRecordFor(page, root, config.Defaults().Drift)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "unknown" {
		t.Fatalf("status: %q", rec.Status)
	}
	if rec.LastVerified != "" {
		t.Fatalf("last_verified should be empty: %q", rec.LastVerified)
	}
	if len(rec.Tags) != 0 {
		t.Fatalf("tags should be empty: %v", rec.Tags)
	}
}

func TestPageRecordExtractsTitleWhenMissing(t *testing.T) {
	root := t.TempDir()
	page := writeFile(t, filepath.Join(root, "concepts", "no-title.md"), "no title at all\n")
	rec, err := PageRecordFor(page, root, config.Defaults().Drift)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Title != "no-title" {
		t.Fatalf("title fallback: %q", rec.Title)
	}
}

func TestPageRecordExplicitCitedFiles(t *testing.T) {
	root := t.TempDir()
	page := writeFile(t, filepath.Join(root, "concepts", "explicit.md"), `---
tags: []
cited_files: [explicitly/declared.go]
---

# Explicit

> s

See `+"`my-app/pkg/types.go`"+` for the body cite.
`)
	dc := config.DriftConfig{RepoPrefixes: []string{"my-app/"}}
	rec, err := PageRecordFor(page, root, dc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.CitedFiles, []string{"explicitly/declared.go"}) {
		t.Fatalf("cited_files: %v", rec.CitedFiles)
	}
}

func TestBuildMetaSkipsAndSorts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "z.md"), "# Z\n\n> s\n")
	writeFile(t, filepath.Join(root, "concepts", "a.md"), "# A\n\n> s\n")
	writeFile(t, filepath.Join(root, "investigations", "m.md"), "---\ntags: [incident]\n---\n# M\n\n> s\n")
	writeFile(t, filepath.Join(root, "_lint", "should-skip.md"), "# Skip\n")
	writeFile(t, filepath.Join(root, "sources", "skip-too.md"), "# Skip\n")
	writeFile(t, filepath.Join(root, "log.md"), "# top-level skipped\n")

	idx, err := BuildMeta(root, config.Defaults().Lint, config.Defaults().Drift)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(idx.Pages))
	for _, p := range idx.Pages {
		got = append(got, p.Slug)
	}
	want := []string{"concepts/a", "concepts/z", "investigations/m"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if idx.Count != 3 {
		t.Fatalf("count: %d", idx.Count)
	}
}

func TestMarshalDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "a.md"), "# A\n\n> s\n")
	idx, err := BuildMeta(root, config.Defaults().Lint, config.Defaults().Drift)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Fatal("Marshal not deterministic")
	}
	if b1[len(b1)-1] != '\n' {
		t.Fatal("Marshal must end with newline")
	}
}
