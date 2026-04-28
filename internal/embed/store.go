package embed

import (
	"context"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// Entry is one indexed chunk: its source page, its raw text, and its
// embedding vector.
type Entry struct {
	Slug   string
	Title  string
	Path   string
	Text   string
	Vector []float32
}

// Store is a flat-file vector index. It's deliberately simple — one gob
// file per wiki, in-memory cosine search. No sqlite-vec FFI, no external
// service. v0.3 can swap a real ANN index in behind this same API.
type Store struct {
	Provider string
	Model    string
	Dim      int
	Entries  []Entry
}

// Hit is a single ranked result.
type Hit struct {
	Entry Entry
	Score float32
}

// Build embeds every chunk via emb and assembles a Store.
func Build(ctx context.Context, emb Embedder, entries []Entry) (*Store, error) {
	if emb == nil {
		return nil, fmt.Errorf("nil embedder")
	}
	if len(entries) == 0 {
		return &Store{Provider: emb.Provider(), Model: emb.Model()}, nil
	}
	chunks := make([]string, len(entries))
	for i, e := range entries {
		chunks[i] = e.Text
	}
	vecs, err := emb.Embed(ctx, chunks)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(entries) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d chunks", len(vecs), len(entries))
	}
	s := &Store{Provider: emb.Provider(), Model: emb.Model()}
	for i := range entries {
		entries[i].Vector = vecs[i]
		if s.Dim == 0 {
			s.Dim = len(vecs[i])
		}
	}
	s.Entries = entries
	return s, nil
}

// Search returns the top-k entries by cosine similarity to qVec.
func (s *Store) Search(qVec []float32, k int) []Hit {
	if s == nil || len(s.Entries) == 0 || k <= 0 {
		return nil
	}
	qNorm := norm(qVec)
	if qNorm == 0 {
		return nil
	}
	type scored struct {
		idx   int
		score float32
	}
	out := make([]scored, 0, len(s.Entries))
	for i, e := range s.Entries {
		score := cosine(qVec, e.Vector, qNorm)
		out = append(out, scored{idx: i, score: score})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].score > out[b].score })
	if k > len(out) {
		k = len(out)
	}
	hits := make([]Hit, 0, k)
	for _, s2 := range out[:k] {
		hits = append(hits, Hit{Entry: s.Entries[s2.idx], Score: s2.score})
	}
	return hits
}

// Save writes the store to disk as a gob.
func (s *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return gob.NewEncoder(f).Encode(s)
}

// Load reads a previously-saved store.
func Load(path string) (*Store, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var s Store
	if err := gob.NewDecoder(f).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func cosine(a, b []float32, aNorm float32) float32 {
	if len(a) != len(b) || aNorm == 0 {
		return 0
	}
	var dot, bSq float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		bSq += float64(b[i]) * float64(b[i])
	}
	bNorm := float32(math.Sqrt(bSq))
	if bNorm == 0 {
		return 0
	}
	return float32(dot) / (aNorm * bNorm)
}

func norm(v []float32) float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	return float32(math.Sqrt(s))
}
