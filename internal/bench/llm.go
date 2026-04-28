package bench

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/llm"
	"github.com/aomerk/keeba/internal/search"
)

// LLMReport is the output of an LLM-driven bench: per-question, per-mode
// answers + token usage from the API response itself (not estimates).
type LLMReport struct {
	When     time.Time
	Provider string
	Model    string
	Rows     []LLMRow
	WikiSum  LLMAggregate
	RawSum   LLMAggregate
}

// LLMRow holds the wiki-mode and raw-mode answers for one question.
type LLMRow struct {
	Question Question
	Wiki     LLMAnswer
	Raw      LLMAnswer
}

// LLMAnswer is a single recorded API response.
type LLMAnswer struct {
	ContextChars int
	InputTokens  int
	OutputTokens int
	Wall         time.Duration
	Confidence   int
	Text         string
}

// LLMAggregate sums input + output tokens + wall across all questions for
// one mode.
type LLMAggregate struct {
	InputTokens  int
	OutputTokens int
	Wall         time.Duration
}

// RunLLM runs the bench through a real LLM evaluator. Returns a typed
// report; the caller decides whether to write markdown.
//
// rawPaths are the directories the raw-mode prompt reads from. Their
// concatenated contents are truncated to maxRawChars to keep request
// payloads under most providers' input limits.
func RunLLM(
	ctx context.Context,
	cfg config.KeebaConfig,
	eval llm.Evaluator,
	qs []Question,
	rawPaths []string,
	topK int,
	maxRawChars int,
) (LLMReport, error) {
	idx, err := search.Build(cfg)
	if err != nil {
		return LLMReport{}, err
	}
	rawBlob := buildRawBlob(cfg, rawPaths, maxRawChars)

	rep := LLMReport{
		When: time.Now().UTC(), Provider: eval.Provider(), Model: eval.Model(),
	}
	for _, q := range qs {
		row := LLMRow{Question: q}

		wikiCtx := buildWikiBlob(idx, q.Text, topK)
		row.Wiki, err = ask(ctx, eval, q.Text, wikiCtx)
		if err != nil {
			return rep, fmt.Errorf("wiki q=%q: %w", q.ID, err)
		}

		row.Raw, err = ask(ctx, eval, q.Text, rawBlob)
		if err != nil {
			return rep, fmt.Errorf("raw q=%q: %w", q.ID, err)
		}

		rep.Rows = append(rep.Rows, row)
		rep.WikiSum.InputTokens += row.Wiki.InputTokens
		rep.WikiSum.OutputTokens += row.Wiki.OutputTokens
		rep.WikiSum.Wall += row.Wiki.Wall
		rep.RawSum.InputTokens += row.Raw.InputTokens
		rep.RawSum.OutputTokens += row.Raw.OutputTokens
		rep.RawSum.Wall += row.Raw.Wall
	}
	return rep, nil
}

// RatioInputTokens returns wiki/raw input-token ratio (smaller = wiki saves more).
func (r LLMReport) RatioInputTokens() float64 {
	if r.RawSum.InputTokens == 0 {
		return 0
	}
	return float64(r.WikiSum.InputTokens) / float64(r.RawSum.InputTokens)
}

// RatioWall returns wiki/raw wall ratio.
func (r LLMReport) RatioWall() float64 {
	if r.RawSum.Wall == 0 {
		return 0
	}
	return float64(r.WikiSum.Wall) / float64(r.RawSum.Wall)
}

// AvgWikiConfidence returns the mean self-rated confidence across wiki-mode
// answers (1-5; 0s are skipped from the average).
func (r LLMReport) AvgWikiConfidence() float64 {
	return avgConfidence(extractConf(r.Rows, true))
}

// AvgRawConfidence returns the mean confidence across raw-mode answers.
func (r LLMReport) AvgRawConfidence() float64 {
	return avgConfidence(extractConf(r.Rows, false))
}

func ask(ctx context.Context, eval llm.Evaluator, question, contextBlob string) (LLMAnswer, error) {
	ans, err := eval.Answer(ctx, question, contextBlob)
	if err != nil {
		return LLMAnswer{}, err
	}
	return LLMAnswer{
		ContextChars: len(contextBlob),
		InputTokens:  ans.InputTokens,
		OutputTokens: ans.OutputTokens,
		Wall:         ans.Wall,
		Confidence:   ans.Confidence,
		Text:         ans.Text,
	}, nil
}

func buildWikiBlob(idx *search.Index, q string, k int) string {
	hits := idx.Query(q, k)
	if len(hits) == 0 {
		return "(no relevant wiki pages found)"
	}
	var sb strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&sb, "## %d. %s (%s, score=%.2f)\n\n%s\n\n", i+1, h.Title, h.Slug, h.Score, h.Snippet)
	}
	return sb.String()
}

func buildRawBlob(cfg config.KeebaConfig, rawPaths []string, maxChars int) string {
	if len(rawPaths) == 0 {
		rawPaths = []string{cfg.WikiRoot}
	}
	var sb strings.Builder
	for _, base := range rawPaths {
		if !filepath.IsAbs(base) {
			base = filepath.Join(cfg.WikiRoot, base)
		}
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if sb.Len() >= maxChars || err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "node_modules" || name == "vendor" || name == "_bench" {
					return fs.SkipDir
				}
				return nil
			}
			if !isTextFile(path) {
				return nil
			}
			b, err := os.ReadFile(path) //nolint:gosec
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(base, path)
			fmt.Fprintf(&sb, "\n--- %s ---\n", rel)
			sb.Write(b)
			return nil
		})
		if sb.Len() >= maxChars {
			break
		}
	}
	out := sb.String()
	if len(out) > maxChars {
		out = out[:maxChars] + "\n[truncated]\n"
	}
	return out
}

func extractConf(rows []LLMRow, wikiSide bool) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		if wikiSide {
			if r.Wiki.Confidence > 0 {
				out = append(out, r.Wiki.Confidence)
			}
		} else if r.Raw.Confidence > 0 {
			out = append(out, r.Raw.Confidence)
		}
	}
	return out
}

func avgConfidence(xs []int) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0
	for _, x := range xs {
		sum += x
	}
	return float64(sum) / float64(len(xs))
}
