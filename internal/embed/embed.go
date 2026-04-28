// Package embed provides keeba's vector-embedding plumbing: an Embedder
// interface, hosted implementations (Voyage, OpenAI), and a deferred
// "local" stub that points users at v0.3 for offline embedding.
//
// v0.2 ships hosted providers as the working path. The locked plan
// (keeba-vision §16) called for sentence-transformers as the eventual
// default; v0.3 lands cybertron + ONNX MiniLM under the same interface so
// the on-disk index carries forward.
package embed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Embedder turns a batch of text chunks into row-aligned vectors. Returning
// vectors of inconsistent dimension is a programming error.
type Embedder interface {
	Embed(ctx context.Context, chunks []string) ([][]float32, error)
	// Provider returns the human-readable provider tag.
	Provider() string
	// Model returns the model identifier the embedder used.
	Model() string
	// Dim returns the dimension of every emitted vector.
	Dim() int
}

// ErrLocalNotImplemented is returned by NewLocal until cybertron + ONNX
// MiniLM lands in v0.3.
var ErrLocalNotImplemented = errors.New(
	"local embedder not implemented in v0.2 — set KEEBA_EMBED_PROVIDER to voyage or openai (v0.3 ships cybertron MiniLM)",
)

// NewFromEnv selects an Embedder based on KEEBA_EMBED_PROVIDER. Recognised
// values: voyage, openai, local. Returns (nil, error) if the chosen
// provider can't be constructed (missing API key, etc).
func NewFromEnv() (Embedder, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("KEEBA_EMBED_PROVIDER")))
	if provider == "" {
		provider = "voyage"
	}
	switch provider {
	case "voyage":
		return NewVoyage()
	case "openai":
		return NewOpenAI()
	case "local":
		return nil, ErrLocalNotImplemented
	default:
		return nil, fmt.Errorf("unknown KEEBA_EMBED_PROVIDER %q (supported: voyage, openai, local)", provider)
	}
}
