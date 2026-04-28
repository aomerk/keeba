package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Voyage embeds chunks via the Voyage AI API.
type Voyage struct {
	APIKey     string
	APIBase    string
	ModelName  string
	HTTPClient *http.Client
	dim        int
	BatchSize  int
}

// NewVoyage builds a Voyage embedder from environment variables. Returns
// (nil, error) when VOYAGE_API_KEY is unset.
func NewVoyage() (*Voyage, error) {
	key := os.Getenv("VOYAGE_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("VOYAGE_API_KEY not set")
	}
	model := os.Getenv("KEEBA_VOYAGE_MODEL")
	if model == "" {
		model = "voyage-3"
	}
	base := os.Getenv("VOYAGE_API_BASE")
	if base == "" {
		base = "https://api.voyageai.com/v1"
	}
	return &Voyage{
		APIKey: key, APIBase: base, ModelName: model,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		BatchSize:  64,
	}, nil
}

// Provider returns "voyage".
func (v *Voyage) Provider() string { return "voyage" }

// Model returns the configured model identifier.
func (v *Voyage) Model() string { return v.ModelName }

// Dim returns the dimensionality of emitted vectors. Zero before first call
// (Voyage doesn't publish a static dim per model and the caller doesn't need
// it until a search runs against a populated index).
func (v *Voyage) Dim() int { return v.dim }

type voyageRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"`
}

type voyageResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed batches chunks through the Voyage API and concatenates results.
func (v *Voyage) Embed(ctx context.Context, chunks []string) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(chunks))
	batch := v.BatchSize
	if batch <= 0 {
		batch = 64
	}
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		body, err := json.Marshal(voyageRequest{Input: chunks[i:end], Model: v.ModelName, InputType: "document"})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "POST", v.APIBase+"/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+v.APIKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := v.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("voyage %d: %s", resp.StatusCode, string(respBody))
		}
		var parsed voyageResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, err
		}
		for _, d := range parsed.Data {
			if v.dim == 0 && len(d.Embedding) > 0 {
				v.dim = len(d.Embedding)
			}
			if len(d.Embedding) != v.dim {
				return nil, fmt.Errorf("voyage returned vector of dim %d; expected %d", len(d.Embedding), v.dim)
			}
			out = append(out, d.Embedding)
		}
	}
	return out, nil
}
