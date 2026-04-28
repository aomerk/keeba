package search

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/embed"
	"github.com/aomerk/keeba/internal/lint"
)

// VectorStorePath returns where keeba persists the embed store, relative to
// the wiki root.
const VectorStorePath = ".keeba-cache/vectors.gob"

// gatherChunks reads the wiki and returns one embed.Entry per indexable
// page. v0.2 uses one chunk per page (whole body); v0.3 will chunk by
// section so larger pages produce finer-grained matches.
func gatherChunks(cfg config.KeebaConfig) ([]embed.Entry, error) {
	pages, err := lint.AllPages(cfg.WikiRoot, cfg.Lint)
	if err != nil {
		return nil, err
	}
	out := make([]embed.Entry, 0, len(pages))
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
		rel, _ := filepath.Rel(cfg.WikiRoot, p)
		slug := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		out = append(out, embed.Entry{
			Slug:  slug,
			Title: title,
			Path:  p,
			Text:  stripped,
		})
	}
	return out, nil
}

// IndexAndPersist embeds every page in the wiki and writes the result to
// `<wiki>/.keeba-cache/vectors.gob`.
func IndexAndPersist(ctx context.Context, cfg config.KeebaConfig, emb embed.Embedder) (int, error) {
	entries, err := gatherChunks(cfg)
	if err != nil {
		return 0, err
	}
	store, err := embed.Build(ctx, emb, entries)
	if err != nil {
		return 0, err
	}
	out := filepath.Join(cfg.WikiRoot, VectorStorePath)
	return len(store.Entries), store.Save(out)
}

// VectorQuery embeds the query, loads the persisted store, and returns
// top-k results.
func VectorQuery(ctx context.Context, cfg config.KeebaConfig, emb embed.Embedder, q string, k int) ([]Hit, error) {
	store, err := embed.Load(filepath.Join(cfg.WikiRoot, VectorStorePath))
	if err != nil {
		return nil, fmt.Errorf("load vector store: %w (did you run `keeba index`?)", err)
	}
	if store.Provider != emb.Provider() || store.Model != emb.Model() {
		return nil, fmt.Errorf(
			"vector store was built with %s/%s but the active embedder is %s/%s — re-run `keeba index`",
			store.Provider, store.Model, emb.Provider(), emb.Model(),
		)
	}
	qVecs, err := emb.Embed(ctx, []string{q})
	if err != nil {
		return nil, err
	}
	if len(qVecs) == 0 {
		return nil, nil
	}
	embHits := store.Search(qVecs[0], k)
	out := make([]Hit, 0, len(embHits))
	for _, h := range embHits {
		out = append(out, Hit{
			Slug:    h.Entry.Slug,
			Title:   h.Entry.Title,
			Path:    h.Entry.Path,
			Score:   float64(h.Score),
			Snippet: snippet(h.Entry.Text, []string{strings.ToLower(strings.SplitN(q, " ", 2)[0])}),
		})
	}
	return out, nil
}
