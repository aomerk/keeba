package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallKeebaOutputStyle_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output-styles", "keeba.md")
	changed, err := installKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected change=true on fresh file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("output style not written: %v", err)
	}
	got := string(body)
	// Frontmatter is the contract — Claude Code reads `name` /
	// `description` from here.
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("output style missing frontmatter:\n%s", got)
	}
	if !strings.Contains(got, "name: keeba") {
		t.Errorf("output style missing `name: keeba` in frontmatter:\n%s", got)
	}
}

func TestInstallKeebaOutputStyle_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keeba.md")
	if _, err := installKeebaOutputStyle(path); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	changed, err := installKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("second install should be no-op")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("idempotent install changed bytes")
	}
}

func TestInstallKeebaOutputStyle_OverwritesStale(t *testing.T) {
	// Older version of the style sitting on disk. Re-install must
	// replace it with the current canonical content. Without overwrite,
	// users would silently keep running the old style after upgrading
	// keeba.
	dir := t.TempDir()
	path := filepath.Join(dir, "keeba.md")
	stale := "---\nname: keeba\ndescription: old\n---\n\nold body\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := installKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected change=true when stale content present")
	}
	body, _ := os.ReadFile(path)
	if string(body) != keebaOutputStyle {
		t.Errorf("stale content not replaced — current canonical not written:\n%s", body)
	}
}

func TestKeebaOutputStyle_PinsCriticalPhrases(t *testing.T) {
	// Style content is the actual lever that attacks output-token
	// bloat. Pin the directives so future edits can't quietly soften
	// them. If any of these disappear, the savings claim weakens.
	for _, want := range []string{
		"name: keeba",
		"No preamble",
		"No closing summary",
		"Quote, don't restate",
		"Conclusion first",
		"Forbidden filler",
		"/output-style keeba",
		// Inter-tool silence — the lever that targets the 20-40%
		// slice of investigation output spent on progress markers
		// nobody reads. Pin so future edits can't quietly soften it.
		"Silence between tool calls",
		"Do NOT write transition prose between tool calls",
		"ONE consolidated answer block",
	} {
		if !strings.Contains(keebaOutputStyle, want) {
			t.Errorf("keebaOutputStyle missing phrase %q", want)
		}
	}
}
