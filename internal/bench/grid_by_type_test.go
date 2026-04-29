package bench

import (
	"strings"
	"testing"
)

const fmHead = "---\ntags: [x]\nstatus: current\nlast_verified: 2026-04-29\n---\n"

func TestRunEncodingGridByType_PartitionsByDetectedType(t *testing.T) {
	cfg := fixtureWiki(t, map[string]string{
		// function: body has Python def
		"concepts/code-page": fmHead + "# code-page\n\n> sum\n\n" +
			"def add(x, y):\n    return x + y\n",
		// narrative: prose with no bullets / no defs
		"concepts/decision": fmHead + "# decision\n\n> sum\n\n" +
			strings.Repeat("We are using BM25 because it works for the corpus shape. ", 30),
		// entity: bullet-fact list
		"concepts/aave": fmHead + "# aave\n\n> sum\n\n" +
			"- chain: ethereum\n- address: 0xabc\n- deployed: 2023\n- status: active\n- website: aave.com\n",
	})

	rep, err := RunEncodingGridByType(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if rep.PageCounts[PageTypeFunction] != 1 {
		t.Errorf("expected 1 function page, got %d", rep.PageCounts[PageTypeFunction])
	}
	if rep.PageCounts[PageTypeNarrative] != 1 {
		t.Errorf("expected 1 narrative page, got %d", rep.PageCounts[PageTypeNarrative])
	}
	if rep.PageCounts[PageTypeEntity] != 1 {
		t.Errorf("expected 1 entity page, got %d", rep.PageCounts[PageTypeEntity])
	}

	// Each type should have a grid with all candidate pipelines evaluated.
	for pt, grid := range rep.Types {
		if len(grid.Reports) != len(CandidatePipelines) {
			t.Errorf("page-type %s: got %d reports, want %d",
				pt, len(grid.Reports), len(CandidatePipelines))
		}
	}
}

func TestRunEncodingGridByType_EmptyWiki(t *testing.T) {
	cfg := fixtureWiki(t, map[string]string{})
	rep, err := RunEncodingGridByType(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(rep.Types) != 0 {
		t.Errorf("empty wiki should produce no type reports, got %v", rep.Types)
	}
}

func TestMarkdownEncodingGridByType_RendersAllPresentTypes(t *testing.T) {
	rep := TypedGridReport{
		Types: map[PageType]GridReport{
			PageTypeFunction: {
				Recommended: "glossary-dedupe+structural-card",
				BestRatio:   3.95,
				Reports: []EncodingReport{
					{Pipeline: "glossary-dedupe+structural-card", TotalRaw: 100, TotalEnc: 25, Pages: []EncodingPage{{Slug: "x"}}},
					{Pipeline: "raw", TotalRaw: 100, TotalEnc: 100, Pages: []EncodingPage{{Slug: "x"}}},
				},
			},
			PageTypeNarrative: {
				Recommended: "glossary-dedupe+md-caveman",
				BestRatio:   1.39,
				Reports: []EncodingReport{
					{Pipeline: "glossary-dedupe+md-caveman", TotalRaw: 100, TotalEnc: 72, Pages: []EncodingPage{{Slug: "y"}}},
				},
			},
		},
		PageCounts: map[PageType]int{PageTypeFunction: 1, PageTypeNarrative: 1},
	}
	md := MarkdownEncodingGridByType(rep)
	if !strings.Contains(md, "function pages (1)") {
		t.Errorf("expected function header, got %q", md)
	}
	if !strings.Contains(md, "narrative pages (1)") {
		t.Errorf("expected narrative header, got %q", md)
	}
	if !strings.Contains(md, "3.95×") {
		t.Errorf("expected function winner ratio, got %q", md)
	}
	if !strings.Contains(md, "1.39×") {
		t.Errorf("expected narrative winner ratio, got %q", md)
	}
}

func TestMarkdownEncodingGridByType_NoPages(t *testing.T) {
	md := MarkdownEncodingGridByType(TypedGridReport{})
	if !strings.Contains(md, "no pages") {
		t.Errorf("expected 'no pages' message, got %q", md)
	}
}

func TestCitedFilesFromFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		fm   map[string]any
		want []string
	}{
		{"missing", map[string]any{}, nil},
		{"wrong type", map[string]any{"cited_files": "not-a-list"}, nil},
		{"valid", map[string]any{"cited_files": []any{"a.go", "b.py"}}, []string{"a.go", "b.py"}},
		{"mixed types", map[string]any{"cited_files": []any{"a.go", 42, "b.py"}}, []string{"a.go", "b.py"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := citedFilesFromFrontmatter(tc.fm)
			if len(got) != len(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
