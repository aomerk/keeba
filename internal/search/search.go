// Package search provides a pure-Go BM25 index over a wiki tree.
//
// v0.1 scope: keyword-based ranking, no embeddings. Vector search lands in
// v0.2 once the embedding-provider plumbing is in place.
package search

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/lint"
)

// BM25 hyperparameters. The standard "classic" defaults — leaving them as
// constants keeps the index reproducible across runs.
const (
	k1 = 1.5
	b  = 0.75
)

// Doc is a single page in the corpus.
type Doc struct {
	Path     string // absolute path
	Slug     string // wiki-relative path without .md
	Title    string
	Body     string // raw body (frontmatter stripped)
	tokens   []string
	tfCounts map[string]int
}

// Hit is a single ranked search result.
type Hit struct {
	Slug    string  `json:"slug"`
	Title   string  `json:"title"`
	Path    string  `json:"path"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// Index is an in-memory BM25 index.
type Index struct {
	docs   []Doc
	avgdl  float64
	df     map[string]int // document frequency for each term
	totalN int
}

// Build scans the wiki and constructs an in-memory BM25 index over every
// linted page (skip rules from cfg.Lint apply).
func Build(cfg config.KeebaConfig) (*Index, error) {
	pages, err := lint.AllPages(cfg.WikiRoot, cfg.Lint)
	if err != nil {
		return nil, err
	}
	idx := &Index{df: map[string]int{}}
	totalLen := 0
	for _, p := range pages {
		body, err := readPageBody(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		stripped := lint.StripFrontmatter(body)
		title := extractTitle(stripped)
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(p), ".md")
		}
		toks := Tokenize(stripped)
		tf := map[string]int{}
		for _, t := range toks {
			tf[t]++
		}
		for term := range tf {
			idx.df[term]++
		}
		rel, _ := filepath.Rel(cfg.WikiRoot, p)
		slug := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		idx.docs = append(idx.docs, Doc{
			Path: p, Slug: slug, Title: title, Body: stripped,
			tokens: toks, tfCounts: tf,
		})
		totalLen += len(toks)
	}
	idx.totalN = len(idx.docs)
	if idx.totalN > 0 {
		idx.avgdl = float64(totalLen) / float64(idx.totalN)
	}
	return idx, nil
}

// N returns the number of indexed documents.
func (i *Index) N() int { return i.totalN }

// Query returns the top-k hits for a free-text query. An empty result slice
// (not error) means no documents matched.
func (i *Index) Query(q string, k int) []Hit {
	if i == nil || i.totalN == 0 || k <= 0 {
		return nil
	}
	terms := Tokenize(q)
	if len(terms) == 0 {
		return nil
	}

	type scored struct {
		idx   int
		score float64
	}
	out := make([]scored, 0, len(i.docs))
	for di, d := range i.docs {
		s := 0.0
		dl := float64(len(d.tokens))
		for _, t := range terms {
			tf := float64(d.tfCounts[t])
			if tf == 0 {
				continue
			}
			n := float64(i.df[t])
			idf := math.Log((float64(i.totalN)-n+0.5)/(n+0.5) + 1)
			norm := tf * (k1 + 1) / (tf + k1*(1-b+b*dl/i.avgdl))
			s += idf * norm
		}
		if s > 0 {
			out = append(out, scored{idx: di, score: s})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].score > out[b].score })
	if k > len(out) {
		k = len(out)
	}
	hits := make([]Hit, 0, k)
	for _, s := range out[:k] {
		d := i.docs[s.idx]
		hits = append(hits, Hit{
			Slug: d.Slug, Title: d.Title, Path: d.Path,
			Score: s.score, Snippet: snippet(d.Body, terms),
		})
	}
	return hits
}

// readPageBody returns the file's contents as a string.
func readPageBody(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // CLI takes user-provided paths
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// extractTitle returns the first `# Heading` line content, or "".
func extractTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

// snippet finds a 200-char window around the first matching term.
func snippet(body string, terms []string) string {
	lower := strings.ToLower(body)
	hit := -1
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 {
			if hit == -1 || i < hit {
				hit = i
			}
		}
	}
	if hit == -1 {
		return firstSnippet(body, 200)
	}
	start := hit - 60
	if start < 0 {
		start = 0
	}
	end := hit + 140
	if end > len(body) {
		end = len(body)
	}
	out := strings.TrimSpace(body[start:end])
	out = strings.ReplaceAll(out, "\n", " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(body) {
		out += "…"
	}
	return out
}

func firstSnippet(body string, n int) string {
	stripped := strings.ReplaceAll(strings.TrimSpace(body), "\n", " ")
	if len(stripped) <= n {
		return stripped
	}
	return stripped[:n] + "…"
}
