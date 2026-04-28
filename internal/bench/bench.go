// Package bench measures the token-and-time saving of querying via the
// keeba wiki versus reading the raw source corpus end-to-end.
//
// v0.1 ships a synthetic but defensible measurement: per question, "wiki
// mode" returns the top-k BM25 chunks; "raw mode" reads every file in the
// configured raw paths and concatenates them. The token count is the
// number of bytes of input an LLM would have to ingest in each mode, which
// is the actual cost driver — whether the LLM ultimately *answers*
// correctly is left to v0.2's eval harness.
package bench

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/search"
)

// CharsPerToken is the OpenAI/Anthropic rule-of-thumb conversion. v0.1 uses
// it for the headline ratio; v0.2 can swap in a real tokenizer.
const CharsPerToken = 4

// Question is one prompt fed to both modes.
type Question struct {
	ID   string
	Text string
}

// DefaultCodeQuestions ships with v0.1 — what an engineer would actually
// ask of a code-project wiki. Editable per-corpus via --questions.
var DefaultCodeQuestions = []Question{
	{ID: "schema", Text: "what is the schema of this project's data model?"},
	{ID: "auth", Text: "how does authentication work in this project?"},
	{ID: "deploy", Text: "how is this project deployed?"},
	{ID: "tests", Text: "where do the tests live and how are they organized?"},
	{ID: "config", Text: "what configuration does this project read at startup?"},
}

// Result is the per-mode-per-question measurement.
type Result struct {
	Question  Question
	Mode      string
	Chars     int
	Tokens    int
	Wall      time.Duration
	ToolCalls int
}

// Report is the aggregate of one bench run.
type Report struct {
	When    time.Time
	N       int
	Wiki    []Result
	Raw     []Result
	WikiSum AggregateRow
	RawSum  AggregateRow
}

// AggregateRow sums Chars/Tokens/Wall across all questions for one mode.
type AggregateRow struct {
	Chars  int
	Tokens int
	Wall   time.Duration
}

// RatioTokens returns wiki/raw token ratio (smaller = wiki saves more).
func (r Report) RatioTokens() float64 {
	if r.RawSum.Tokens == 0 {
		return 0
	}
	return float64(r.WikiSum.Tokens) / float64(r.RawSum.Tokens)
}

// RatioWall returns wiki/raw wall-time ratio.
func (r Report) RatioWall() float64 {
	if r.RawSum.Wall == 0 {
		return 0
	}
	return float64(r.WikiSum.Wall) / float64(r.RawSum.Wall)
}

// Run executes the benchmark over the given questions.
//
// rawPaths are absolute or wiki-root-relative paths that "raw mode" reads.
// If rawPaths is empty the bench falls back to the wiki root itself, which
// makes the comparison degenerate but still runnable for smoke tests.
func Run(cfg config.KeebaConfig, qs []Question, rawPaths []string, topK int) (Report, error) {
	idx, err := search.Build(cfg)
	if err != nil {
		return Report{}, err
	}

	rep := Report{When: time.Now().UTC(), N: len(qs)}
	for _, q := range qs {
		rep.Wiki = append(rep.Wiki, runWiki(idx, q, topK))
	}
	for _, q := range qs {
		r, err := runRaw(cfg, q, rawPaths)
		if err != nil {
			return rep, err
		}
		rep.Raw = append(rep.Raw, r)
	}

	for _, r := range rep.Wiki {
		rep.WikiSum.Chars += r.Chars
		rep.WikiSum.Tokens += r.Tokens
		rep.WikiSum.Wall += r.Wall
	}
	for _, r := range rep.Raw {
		rep.RawSum.Chars += r.Chars
		rep.RawSum.Tokens += r.Tokens
		rep.RawSum.Wall += r.Wall
	}
	return rep, nil
}

func runWiki(idx *search.Index, q Question, k int) Result {
	t0 := time.Now()
	hits := idx.Query(q.Text, k)
	wall := time.Since(t0)
	chars := 0
	for _, h := range hits {
		chars += len(h.Title) + len(h.Slug) + len(h.Snippet)
	}
	return Result{
		Question: q, Mode: "wiki",
		Chars: chars, Tokens: chars / CharsPerToken,
		Wall: wall, ToolCalls: 1,
	}
}

func runRaw(cfg config.KeebaConfig, q Question, rawPaths []string) (Result, error) {
	if len(rawPaths) == 0 {
		rawPaths = []string{cfg.WikiRoot}
	}
	t0 := time.Now()
	chars := 0
	for _, base := range rawPaths {
		if !filepath.IsAbs(base) {
			base = filepath.Join(cfg.WikiRoot, base)
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "node_modules" || name == "vendor" {
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
			chars += len(b)
			return nil
		})
		if err != nil {
			return Result{}, fmt.Errorf("walk %s: %w", base, err)
		}
	}
	wall := time.Since(t0)
	return Result{
		Question: q, Mode: "raw",
		Chars: chars, Tokens: chars / CharsPerToken,
		Wall: wall, ToolCalls: 1,
	}, nil
}

func isTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".go", ".py", ".js", ".ts", ".tsx", ".jsx",
		".rs", ".java", ".kt", ".rb", ".sh", ".yaml", ".yml", ".json",
		".toml", ".ini", ".cfg", ".html", ".css", ".sql":
		return true
	}
	return false
}
