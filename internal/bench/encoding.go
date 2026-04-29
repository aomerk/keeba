package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/encoding"
	"github.com/aomerk/keeba/internal/lint"
)

// EncodingReport summarizes the compression achieved by running an
// encoding pipeline over every page in the wiki.
type EncodingReport struct {
	Pipeline string
	Pages    []EncodingPage
	TotalRaw int
	TotalEnc int
}

// EncodingPage is the raw / encoded byte count for one page.
type EncodingPage struct {
	Slug     string
	RawChars int
	EncChars int
}

// Ratio returns raw/encoded — values > 1 mean the encoder shrank the
// content. Returns 0 if no encoded bytes (degenerate / empty wiki).
func (r EncodingReport) Ratio() float64 {
	if r.TotalEnc == 0 {
		return 0
	}
	return float64(r.TotalRaw) / float64(r.TotalEnc)
}

// Ratio returns the raw/encoded ratio for a single page.
func (p EncodingPage) Ratio() float64 {
	if p.EncChars == 0 {
		return 0
	}
	return float64(p.RawChars) / float64(p.EncChars)
}

// RunEncoding loads every page in the wiki, runs the configured encoding
// pipeline over the body (frontmatter is stripped first), and reports the
// raw vs encoded character counts. The pipeline's Fit hook is called on
// the full corpus before any Encode, so glossary-style stateful encoders
// see real frequency data.
func RunEncoding(cfg config.KeebaConfig, spec string) (EncodingReport, error) {
	pipeline, err := encoding.BuildPipeline(spec)
	if err != nil {
		return EncodingReport{}, err
	}

	pages, err := lint.AllPages(cfg.WikiRoot, cfg.Lint)
	if err != nil {
		return EncodingReport{}, err
	}

	bodies := make([]string, 0, len(pages))
	slugs := make([]string, 0, len(pages))
	for _, p := range pages {
		raw, err := os.ReadFile(p) //nolint:gosec
		if err != nil {
			return EncodingReport{}, fmt.Errorf("read %s: %w", p, err)
		}
		body := lint.StripFrontmatter(string(raw))
		bodies = append(bodies, body)
		rel, _ := filepath.Rel(cfg.WikiRoot, p)
		slugs = append(slugs, strings.TrimSuffix(filepath.ToSlash(rel), ".md"))
	}

	if err := pipeline.Fit(bodies); err != nil {
		return EncodingReport{}, fmt.Errorf("fit %s: %w", pipeline.Name(), err)
	}

	rep := EncodingReport{Pipeline: pipeline.Name()}
	for i, body := range bodies {
		encoded, err := pipeline.Encode(body)
		if err != nil {
			return rep, fmt.Errorf("encode %s: %w", slugs[i], err)
		}
		rep.Pages = append(rep.Pages, EncodingPage{
			Slug:     slugs[i],
			RawChars: len(body),
			EncChars: len(encoded),
		})
		rep.TotalRaw += len(body)
		rep.TotalEnc += len(encoded)
	}
	return rep, nil
}
