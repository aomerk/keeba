package bench

import (
	"sort"

	"github.com/aomerk/keeba/internal/config"
)

// CandidatePipelines lists the encoding stacks `keeba bench --encoding-grid`
// evaluates by default. Keep in sync with plan §10 + the empirical winners
// from docs/encoding-bench-2026-04-29.md:
//   - "raw" baseline (no-op)
//   - narrative-page candidates (caveman, glossary stacked)
//   - function/code-page candidates (structural-card, glossary stacked)
//   - entity/fact-page candidate (dense-tuple)
//
// Order is stable so grid output diffs cleanly across runs.
var CandidatePipelines = []string{
	"raw",
	"caveman",
	"glossary",
	"glossary,caveman",
	"structural-card",
	"glossary,structural-card",
	"dense-tuple",
}

// CompressionCap is plan §10's "don't compress past 4× — quality cliff"
// rule, with a small tolerance to allow plans like glossary+structural-card
// (4.15× on CSN) to still rank as winners. Anything past CompressionCap is
// treated as suspicious and excluded from the recommendation.
const CompressionCap = 4.5

// GridReport is the per-pipeline result of the encoding grid.
type GridReport struct {
	Reports     []EncodingReport
	Recommended string  // pipeline name with the highest ratio under CompressionCap
	BestRatio   float64 // ratio of the recommendation
}

// RunEncodingGrid runs every CandidatePipelines entry against the wiki
// and returns a sorted (highest-ratio-first) GridReport plus the
// recommendation that respects CompressionCap.
func RunEncodingGrid(cfg config.KeebaConfig) (GridReport, error) {
	out := GridReport{}
	for _, spec := range CandidatePipelines {
		rep, err := RunEncoding(cfg, spec)
		if err != nil {
			return out, err
		}
		out.Reports = append(out.Reports, rep)
	}

	sort.SliceStable(out.Reports, func(i, j int) bool {
		return out.Reports[i].Ratio() > out.Reports[j].Ratio()
	})

	for _, r := range out.Reports {
		ratio := r.Ratio()
		if ratio <= 0 {
			continue
		}
		if ratio > CompressionCap {
			// Suspect the pipeline lost too much signal — skip per plan §10.
			continue
		}
		out.Recommended = r.Pipeline
		out.BestRatio = ratio
		break
	}
	return out, nil
}
