package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

func TestApplyEncoding_NoConfigIsNoop(t *testing.T) {
	body := "Some prose body that should pass through unchanged."
	got := applyEncoding(body, nil, config.EncodingConfig{})
	if got.Body != body {
		t.Errorf("Body changed: got %q, want %q", got.Body, body)
	}
	if got.Pipeline != "" {
		t.Errorf("Pipeline should be empty when no encoding configured, got %q", got.Pipeline)
	}
}

func TestApplyEncoding_NarrativePipelineApplied(t *testing.T) {
	body := "The quick brown fox is jumping over a lazy dog."
	cfg := config.EncodingConfig{Narrative: "caveman"}
	got := applyEncoding(body, nil, cfg)
	if got.Body == body {
		t.Errorf("expected caveman to compress: %q", got.Body)
	}
	if got.Pipeline != "md-caveman" {
		t.Errorf("Pipeline = %q, want md-caveman", got.Pipeline)
	}
	if string(got.PageType) != "narrative" {
		t.Errorf("PageType = %q, want narrative", got.PageType)
	}
}

func TestApplyEncoding_FunctionPagePicksFunctionPipeline(t *testing.T) {
	cfg := config.EncodingConfig{
		Function:  "structural-card",
		Narrative: "caveman",
	}
	body := "def add(x, y):\n    return x + y\n"
	got := applyEncoding(body, []string{"src/foo.py"}, cfg)
	if got.Pipeline != "structural-card" {
		t.Errorf("expected structural-card on function page, got %q", got.Pipeline)
	}
	if string(got.PageType) != "function" {
		t.Errorf("PageType = %q, want function", got.PageType)
	}
}

func TestApplyEncoding_BadPipelineDegradesGracefully(t *testing.T) {
	body := "anything"
	cfg := config.EncodingConfig{Narrative: "definitely-not-a-real-pipeline"}
	got := applyEncoding(body, nil, cfg)
	if got.Body != body {
		t.Errorf("body should pass through on bad config, got %q", got.Body)
	}
	if got.Pipeline != "" {
		t.Errorf("Pipeline should be empty when build fails, got %q", got.Pipeline)
	}
}

func TestImportFromRepoWithEncoding_AddsPageTypeAndEncodingFrontmatter(t *testing.T) {
	repo := t.TempDir()
	wiki := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"),
		[]byte("# project\n\n> sum\n\nThe quick brown fox is jumping over a lazy dog.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.EncodingConfig{Narrative: "caveman"}
	res, err := ImportFromRepoWithEncoding(wiki, repo, "myproj", cfg)
	if err != nil {
		t.Fatalf("ImportFromRepoWithEncoding: %v", err)
	}
	if len(res.Imported) == 0 {
		t.Fatalf("nothing imported: %+v", res)
	}
	dest := filepath.Join(wiki, "concepts", "readme.md")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "page_type: narrative") {
		t.Errorf("missing page_type frontmatter:\n%s", s)
	}
	if !strings.Contains(s, "encoding: md-caveman") {
		t.Errorf("missing encoding frontmatter:\n%s", s)
	}
}

func TestImportFromRepo_WithoutEncodingStillRecordsPageType(t *testing.T) {
	repo := t.TempDir()
	wiki := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"),
		[]byte("# project\n\n> sum\n\nSome flowing prose.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use the no-encoding entrypoint.
	if _, err := ImportFromRepo(wiki, repo, "myproj"); err != nil {
		t.Fatalf("ImportFromRepo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wiki, "concepts", "readme.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "page_type: narrative") {
		t.Errorf("page_type should be recorded even without encoding:\n%s", s)
	}
	if strings.Contains(s, "encoding:") {
		t.Errorf("encoding frontmatter shouldn't appear when no pipeline ran:\n%s", s)
	}
}

func TestCitedFileFromOrigin(t *testing.T) {
	if got := citedFileFromOrigin("repo", "src/foo.go"); got != "repo/src/foo.go" {
		t.Errorf("got %q", got)
	}
}
