package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/encoding"
	"github.com/aomerk/keeba/internal/lint"
)

// PageType is re-exported from internal/encoding so both bench and
// scaffold can import the type without a cycle. The constants below
// preserve the bench package's public API for existing callers
// (the CLI uses bench.PageTypeFunction).
type PageType = encoding.PageType

// Page-type constants re-exported from internal/encoding.
const (
	PageTypeFunction  = encoding.PageTypeFunction
	PageTypeEntity    = encoding.PageTypeEntity
	PageTypeNarrative = encoding.PageTypeNarrative
)

// pageTypeOrder controls the deterministic order page-types appear in the
// grid output (and the order winners are written to keeba.config.yaml).
var pageTypeOrder = []PageType{PageTypeFunction, PageTypeEntity, PageTypeNarrative}

// pageRecord is one wiki page with its detected type and stripped body.
type pageRecord struct {
	Slug     string
	Body     string
	PageType PageType
}

// loadPagesWithType reads every wiki page, strips frontmatter, and tags
// each with its detected PageType. cited_files frontmatter is consulted
// for stronger function-page detection on imported code pages.
func loadPagesWithType(cfg config.KeebaConfig) ([]pageRecord, error) {
	paths, err := lint.AllPages(cfg.WikiRoot, cfg.Lint)
	if err != nil {
		return nil, err
	}
	out := make([]pageRecord, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		rawStr := string(raw)
		body := lint.StripFrontmatter(rawStr)
		fm := lint.ExtractFrontmatter(rawStr)
		cited := citedFilesFromFrontmatter(fm)

		rel, _ := filepath.Rel(cfg.WikiRoot, p)
		slug := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		out = append(out, pageRecord{
			Slug:     slug,
			Body:     body,
			PageType: encoding.DetectPageType(body, cited),
		})
	}
	return out, nil
}

func citedFilesFromFrontmatter(fm map[string]any) []string {
	raw, ok := fm["cited_files"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// TypedGridReport is the per-page-type encoding-grid result. Keys are
// page types; values are the GridReport for the pages of that type.
type TypedGridReport struct {
	Types map[PageType]GridReport
	// PageCounts is the number of pages classified into each type
	// (informational; useful for "X function pages, Y narrative pages").
	PageCounts map[PageType]int
}

// RunEncodingGridByType partitions wiki pages by detected PageType and
// runs the candidate-pipeline grid against each partition independently.
// Empty partitions are omitted from the report. Plan §10's central claim
// — page-type matters more than aggressive compression — relies on this.
func RunEncodingGridByType(cfg config.KeebaConfig) (TypedGridReport, error) {
	out := TypedGridReport{
		Types:      map[PageType]GridReport{},
		PageCounts: map[PageType]int{},
	}

	pages, err := loadPagesWithType(cfg)
	if err != nil {
		return out, err
	}
	if len(pages) == 0 {
		return out, nil
	}

	// Partition.
	groups := map[PageType][]pageRecord{}
	for _, pg := range pages {
		groups[pg.PageType] = append(groups[pg.PageType], pg)
	}
	for pt, grp := range groups {
		out.PageCounts[pt] = len(grp)
	}

	// Run the grid per partition.
	for pt, grp := range groups {
		report := GridReport{}
		for _, spec := range CandidatePipelines {
			rep, err := runEncodingOnRecords(spec, grp)
			if err != nil {
				return out, fmt.Errorf("page-type %s, pipeline %s: %w", pt, spec, err)
			}
			report.Reports = append(report.Reports, rep)
		}
		sort.SliceStable(report.Reports, func(i, j int) bool {
			return report.Reports[i].Ratio() > report.Reports[j].Ratio()
		})
		for _, r := range report.Reports {
			ratio := r.Ratio()
			if ratio <= 0 || ratio > CompressionCap {
				continue
			}
			report.Recommended = r.Pipeline
			report.BestRatio = ratio
			break
		}
		out.Types[pt] = report
	}
	return out, nil
}

// runEncodingOnRecords is RunEncoding's core, but operating on an
// already-loaded slice of pageRecords instead of re-walking the wiki.
// Lets the per-type grid avoid N×K re-reads.
func runEncodingOnRecords(spec string, pages []pageRecord) (EncodingReport, error) {
	pipeline, err := encoding.BuildPipeline(spec)
	if err != nil {
		return EncodingReport{}, err
	}

	bodies := make([]string, len(pages))
	for i, p := range pages {
		bodies[i] = p.Body
	}
	if err := pipeline.Fit(bodies); err != nil {
		return EncodingReport{}, fmt.Errorf("fit %s: %w", pipeline.Name(), err)
	}

	rep := EncodingReport{Pipeline: pipeline.Name()}
	for i, body := range bodies {
		encoded, err := pipeline.Encode(body)
		if err != nil {
			return rep, fmt.Errorf("encode %s: %w", pages[i].Slug, err)
		}
		rep.Pages = append(rep.Pages, EncodingPage{
			Slug:     pages[i].Slug,
			RawChars: len(body),
			EncChars: len(encoded),
		})
		rep.TotalRaw += len(body)
		rep.TotalEnc += len(encoded)
	}
	return rep, nil
}
