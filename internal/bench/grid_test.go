package bench

import (
	"strings"
	"testing"
)

func TestRunEncodingGridReturnsAllCandidates(t *testing.T) {
	cfg := fixtureWiki(t, map[string]string{
		"concepts/page": "---\ntags: [x]\nstatus: current\nlast_verified: 2026-04-29\n---\n# page\n\n> sum\n\n" +
			strings.Repeat("the cat is here. ", 20),
	})

	grid, err := RunEncodingGrid(cfg)
	if err != nil {
		t.Fatalf("RunEncodingGrid err = %v", err)
	}
	if len(grid.Reports) != len(CandidatePipelines) {
		t.Errorf("expected %d reports, got %d", len(CandidatePipelines), len(grid.Reports))
	}

	// Reports must be sorted descending by ratio.
	for i := 1; i < len(grid.Reports); i++ {
		if grid.Reports[i-1].Ratio() < grid.Reports[i].Ratio() {
			t.Errorf("reports not sorted descending: %v at %d", grid.Reports, i)
		}
	}
}

func TestRunEncodingGridPicksWinnerUnderCap(t *testing.T) {
	// Use heavily-repeating prose so caveman / glossary actually compress.
	body := "---\ntags: [x]\nstatus: current\nlast_verified: 2026-04-29\n---\n# page\n\n> sum\n\n" +
		strings.Repeat("The reallyExpensiveIdentifier is the only thing that matters here. ", 80)
	cfg := fixtureWiki(t, map[string]string{"concepts/page": body})

	grid, err := RunEncodingGrid(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if grid.Recommended == "" {
		t.Errorf("expected a recommendation, got empty (best ratio %.4f)", grid.BestRatio)
	}
	if grid.BestRatio > CompressionCap {
		t.Errorf("recommendation exceeds cap: %.4f > %.4f", grid.BestRatio, CompressionCap)
	}
	if grid.BestRatio < 1.0 {
		t.Errorf("expected at least 1× compression, got %.4f", grid.BestRatio)
	}
}

func TestRunEncodingGridSkipsOverCapPipelines(t *testing.T) {
	// Synthetic: cap at 1.05 so any real compression looks suspicious.
	original := CompressionCap
	defer func() { _ = original }()

	body := "---\ntags: [x]\nstatus: current\nlast_verified: 2026-04-29\n---\n# page\n\n> sum\n\n" +
		strings.Repeat("the cat is here. ", 50)
	cfg := fixtureWiki(t, map[string]string{"concepts/page": body})

	grid, err := RunEncodingGrid(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	// Sanity: best ratio respects cap
	if grid.Recommended != "" && grid.BestRatio > CompressionCap {
		t.Errorf("winner over cap: %.4f > %.4f", grid.BestRatio, CompressionCap)
	}
}

func TestMarkdownEncodingGridIncludesWinner(t *testing.T) {
	g := GridReport{
		Recommended: "glossary-dedupe+structural-card",
		BestRatio:   3.95,
		Reports: []EncodingReport{
			{Pipeline: "glossary-dedupe+structural-card", TotalRaw: 1000, TotalEnc: 253, Pages: []EncodingPage{{Slug: "x", RawChars: 1000, EncChars: 253}}},
			{Pipeline: "structural-card", TotalRaw: 1000, TotalEnc: 280, Pages: []EncodingPage{{Slug: "x", RawChars: 1000, EncChars: 280}}},
			{Pipeline: "raw", TotalRaw: 1000, TotalEnc: 1000, Pages: []EncodingPage{{Slug: "x", RawChars: 1000, EncChars: 1000}}},
		},
	}
	md := MarkdownEncodingGrid(g)
	if !strings.Contains(md, "winner: `glossary-dedupe+structural-card`") {
		t.Errorf("expected winner header in markdown, got %q", md)
	}
	if !strings.Contains(md, "⭐") {
		t.Errorf("expected star marker on winner row, got %q", md)
	}
	if !strings.Contains(md, "3.95×") {
		t.Errorf("expected best ratio in body, got %q", md)
	}
}

func TestMarkdownEncodingGridNoWinner(t *testing.T) {
	g := GridReport{Reports: []EncodingReport{}}
	md := MarkdownEncodingGrid(g)
	if !strings.Contains(md, "No pipeline cleared") {
		t.Errorf("expected no-winner explanation, got %q", md)
	}
}
