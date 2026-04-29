package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

// fixtureWiki writes a minimal wiki tree under t.TempDir and returns a
// loaded config rooted there.
func fixtureWiki(t *testing.T, pages map[string]string) config.KeebaConfig {
	t.Helper()
	root := t.TempDir()

	// Minimum directory structure that lint.AllPages walks.
	for slug, body := range pages {
		dir := filepath.Join(root, filepath.Dir(slug))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, slug+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", slug, err)
		}
	}

	cfg := config.Defaults()
	cfg.WikiRoot = root
	return cfg
}

func TestRunEncodingRawIsNoop(t *testing.T) {
	cfg := fixtureWiki(t, map[string]string{
		"concepts/auth": "---\ntags: [auth]\nstatus: current\nlast_verified: 2026-04-29\n---\n# auth\n\n> what auth means\n\nthe service is using JWT tokens",
	})

	rep, err := RunEncoding(cfg, "raw")
	if err != nil {
		t.Fatalf("RunEncoding err = %v", err)
	}
	if rep.Pipeline != "raw" {
		t.Errorf("Pipeline = %q, want %q", rep.Pipeline, "raw")
	}
	if rep.Ratio() != 1.0 {
		t.Errorf("raw pipeline should yield 1.0× ratio, got %.4f", rep.Ratio())
	}
	if len(rep.Pages) != 1 {
		t.Errorf("expected 1 page, got %d", len(rep.Pages))
	}
}

func TestRunEncodingCavemanCompresses(t *testing.T) {
	body := "---\ntags: [x]\nstatus: current\nlast_verified: 2026-04-29\n---\n# title\n\n> the summary line\n\n" +
		strings.Repeat("The quick brown fox is jumping over a lazy dog. ", 20)
	cfg := fixtureWiki(t, map[string]string{"concepts/long": body})

	rep, err := RunEncoding(cfg, "caveman")
	if err != nil {
		t.Fatalf("RunEncoding err = %v", err)
	}
	if rep.Ratio() <= 1.0 {
		t.Errorf("caveman should compress: ratio = %.4f", rep.Ratio())
	}
	if rep.TotalEnc >= rep.TotalRaw {
		t.Errorf("encoded should be smaller: raw=%d enc=%d", rep.TotalRaw, rep.TotalEnc)
	}
}

func TestRunEncodingUnknownPipelineErrors(t *testing.T) {
	cfg := fixtureWiki(t, map[string]string{"concepts/x": "body"})
	_, err := RunEncoding(cfg, "definitely-not-real")
	if err == nil {
		t.Error("expected error on unknown pipeline")
	}
}

func TestRunEncodingMultiPageAggregates(t *testing.T) {
	cfg := fixtureWiki(t, map[string]string{
		"concepts/a": "---\ntags: [a]\nstatus: current\nlast_verified: 2026-04-29\n---\n# a\n\n> sa\n\n" + strings.Repeat("The cat is here. ", 10),
		"concepts/b": "---\ntags: [b]\nstatus: current\nlast_verified: 2026-04-29\n---\n# b\n\n> sb\n\n" + strings.Repeat("The dog is there. ", 10),
	})

	rep, err := RunEncoding(cfg, "caveman")
	if err != nil {
		t.Fatalf("RunEncoding err = %v", err)
	}
	if len(rep.Pages) != 2 {
		t.Errorf("expected 2 pages, got %d", len(rep.Pages))
	}
	wantTotalRaw := 0
	wantTotalEnc := 0
	for _, p := range rep.Pages {
		wantTotalRaw += p.RawChars
		wantTotalEnc += p.EncChars
	}
	if rep.TotalRaw != wantTotalRaw || rep.TotalEnc != wantTotalEnc {
		t.Errorf("aggregates mismatch: report (%d, %d) vs sum-of-pages (%d, %d)",
			rep.TotalRaw, rep.TotalEnc, wantTotalRaw, wantTotalEnc)
	}
}

func TestEncodingReportEmptyRatio(t *testing.T) {
	r := EncodingReport{}
	if got := r.Ratio(); got != 0 {
		t.Errorf("empty report ratio = %v, want 0", got)
	}
	if got := (EncodingPage{}).Ratio(); got != 0 {
		t.Errorf("empty page ratio = %v, want 0", got)
	}
}

func TestMarkdownEncodingIncludesPipelineAndRatio(t *testing.T) {
	rep := EncodingReport{
		Pipeline: "glossary+structural-card",
		Pages:    []EncodingPage{{Slug: "concepts/auth", RawChars: 1000, EncChars: 250}},
		TotalRaw: 1000,
		TotalEnc: 250,
	}
	md := MarkdownEncoding(rep)
	if !strings.Contains(md, "glossary+structural-card") {
		t.Errorf("expected pipeline name in markdown, got %q", md)
	}
	if !strings.Contains(md, "4.00×") {
		t.Errorf("expected 4.00× ratio in markdown, got %q", md)
	}
	if !strings.Contains(md, "concepts/auth") {
		t.Errorf("expected page slug in table, got %q", md)
	}
}
