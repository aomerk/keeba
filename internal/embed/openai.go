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

// OpenAI embeds chunks via OpenAI's embeddings API.
type OpenAI struct {
	APIKey     string
	APIBase    string
	ModelName  string
	HTTPClient *http.Client
	dim        int
	BatchSize  int
}

// NewOpenAI builds an OpenAI embedder from environment variables. Returns
// (nil, error) when OPENAI_API_KEY is unset.
func NewOpenAI() (*OpenAI, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	model := os.Getenv("KEEBA_OPENAI_MODEL")
	if model == "" {
		model = "text-embedding-3-small"
	}
	base := os.Getenv("OPENAI_API_BASE")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return &OpenAI{
		APIKey: key, APIBase: base, ModelName: model,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		BatchSize:  100,
	}, nil
}

// Provider returns "openai".
func (o *OpenAI) Provider() string { return "openai" }

// Model returns the configured model identifier.
func (o *OpenAI) Model() string { return o.ModelName }

// Dim returns the dimensionality of emitted vectors.
func (o *OpenAI) Dim() int { return o.dim }

type openaiRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type openaiResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed batches chunks through the OpenAI API and concatenates results.
func (o *OpenAI) Embed(ctx context.Context, chunks []string) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(chunks))
	batch := o.BatchSize
	if batch <= 0 {
		batch = 100
	}
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		body, err := json.Marshal(openaiRequest{Input: chunks[i:end], Model: o.ModelName})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "POST", o.APIBase+"/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("openai %d: %s", resp.StatusCode, string(respBody))
		}
		var parsed openaiResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, err
		}
		for _, d := range parsed.Data {
			if o.dim == 0 && len(d.Embedding) > 0 {
				o.dim = len(d.Embedding)
			}
			if len(d.Embedding) != o.dim {
				return nil, fmt.Errorf("openai returned vector of dim %d; expected %d", len(d.Embedding), o.dim)
			}
			out = append(out, d.Embedding)
		}
	}
	return out, nil
}
