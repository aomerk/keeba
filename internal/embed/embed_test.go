package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeEmb is a deterministic Embedder for store tests.
type fakeEmb struct {
	dim int
}

func (f *fakeEmb) Provider() string { return "fake" }
func (f *fakeEmb) Model() string    { return "fake-1" }
func (f *fakeEmb) Dim() int         { return f.dim }
func (f *fakeEmb) Embed(_ context.Context, chunks []string) ([][]float32, error) {
	out := make([][]float32, len(chunks))
	for i, c := range chunks {
		v := make([]float32, f.dim)
		for j := range v {
			// deterministic pseudo-vector based on content
			v[j] = float32((int(c[0])+i+j)%7) / 6.0
		}
		out[i] = v
	}
	return out, nil
}

func TestBuildAndSearch(t *testing.T) {
	emb := &fakeEmb{dim: 4}
	entries := []Entry{
		{Slug: "a", Title: "Alpha", Text: "auth and JWT"},
		{Slug: "b", Title: "Beta", Text: "billing and stripe"},
		{Slug: "c", Title: "Gamma", Text: "deployment and helm"},
	}
	store, err := Build(context.Background(), emb, entries)
	if err != nil {
		t.Fatal(err)
	}
	if store.Dim != 4 {
		t.Fatalf("dim: %d", store.Dim)
	}
	if len(store.Entries) != 3 {
		t.Fatalf("entries: %d", len(store.Entries))
	}
	for _, e := range store.Entries {
		if len(e.Vector) != 4 {
			t.Fatalf("missing vector for %s", e.Slug)
		}
	}

	// Query: top-2 should rank highest-cosine first.
	qVec, _ := emb.Embed(context.Background(), []string{"auth and JWT"})
	hits := store.Search(qVec[0], 2)
	if len(hits) != 2 {
		t.Fatalf("hits: %d", len(hits))
	}
	if hits[0].Entry.Slug != "a" {
		t.Fatalf("expected slug a top, got %s", hits[0].Entry.Slug)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	emb := &fakeEmb{dim: 8}
	entries := []Entry{{Slug: "a", Title: "A", Text: "auth"}}
	store, err := Build(context.Background(), emb, entries)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "store.gob")
	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.Entries[0].Vector, loaded.Entries[0].Vector) {
		t.Fatalf("vectors differ: %v vs %v", store.Entries[0].Vector, loaded.Entries[0].Vector)
	}
}

func TestSearchEmptyStore(t *testing.T) {
	var s Store
	if hits := s.Search([]float32{1, 0, 0}, 5); hits != nil {
		t.Fatalf("expected nil, got %v", hits)
	}
}

func TestSearchZeroQuery(t *testing.T) {
	emb := &fakeEmb{dim: 4}
	entries := []Entry{{Slug: "a", Title: "A", Text: "auth"}}
	store, _ := Build(context.Background(), emb, entries)
	if hits := store.Search([]float32{0, 0, 0, 0}, 5); hits != nil {
		t.Fatalf("zero query should return nothing, got %v", hits)
	}
}

func TestVoyageHTTPRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testkey" {
			t.Errorf("auth header missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{1, 0, 0, 0}},
				{"embedding": []float32{0, 1, 0, 0}},
			},
		})
	}))
	defer srv.Close()
	v := &Voyage{
		APIKey: "testkey", APIBase: srv.URL, ModelName: "voyage-test",
		HTTPClient: srv.Client(), BatchSize: 64,
	}
	vecs, err := v.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 4 {
		t.Fatalf("vecs: %v", vecs)
	}
	if v.Dim() != 4 {
		t.Errorf("dim: %d", v.Dim())
	}
}

func TestOpenAIHTTPRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{1, 0, 0}},
			},
		})
	}))
	defer srv.Close()
	o := &OpenAI{
		APIKey: "testkey", APIBase: srv.URL, ModelName: "test-emb",
		HTTPClient: srv.Client(), BatchSize: 100,
	}
	vecs, err := o.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 {
		t.Fatalf("vecs: %v", vecs)
	}
}

func TestNewFromEnvLocalDeferred(t *testing.T) {
	t.Setenv("KEEBA_EMBED_PROVIDER", "local")
	_, err := NewFromEnv()
	if err == nil {
		t.Fatal("expected error for local provider in v0.2")
	}
}

func TestNewFromEnvUnknownProvider(t *testing.T) {
	t.Setenv("KEEBA_EMBED_PROVIDER", "made-up")
	_, err := NewFromEnv()
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
