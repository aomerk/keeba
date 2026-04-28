// Package llm wraps the Anthropic Messages API for keeba's bench. Other
// providers (OpenAI, etc.) can land alongside this file behind the same
// Evaluator interface.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Answer is one LLM response with usage + timing.
type Answer struct {
	Text         string
	InputTokens  int
	OutputTokens int
	Wall         time.Duration
	Confidence   int // 1-5; 0 if not parsed
}

// Evaluator answers one question given a context blob. Implementations are
// stateless — bench creates one per run and reuses it across questions.
type Evaluator interface {
	Answer(ctx context.Context, question, contextBlob string) (Answer, error)
	Provider() string
	Model() string
}

// Anthropic is an Evaluator backed by the Anthropic Messages API.
type Anthropic struct {
	APIKey      string
	APIBase     string
	ModelName   string
	HTTPTimeout time.Duration
	HTTPClient  *http.Client
	MaxTokens   int
}

// NewAnthropic builds an Anthropic client from environment configuration.
// Returns (nil, nil) when ANTHROPIC_API_KEY is unset — callers should treat
// that as "skip LLM mode" rather than a hard error.
func NewAnthropic() (*Anthropic, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, nil
	}
	model := os.Getenv("KEEBA_LLM_MODEL")
	if model == "" {
		model = "claude-haiku-4-5"
	}
	base := os.Getenv("ANTHROPIC_API_BASE")
	if base == "" {
		base = "https://api.anthropic.com/v1"
	}
	return &Anthropic{
		APIKey:      key,
		APIBase:     base,
		ModelName:   model,
		HTTPTimeout: 60 * time.Second,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
		MaxTokens:   1024,
	}, nil
}

// Provider returns the human-readable provider tag.
func (a *Anthropic) Provider() string { return "anthropic" }

// Model returns the configured model identifier.
func (a *Anthropic) Model() string { return a.ModelName }

const benchSystemPrompt = `You answer technical questions about a codebase or product. The user pastes context — either a curated wiki or a raw source dump. Answer concisely from that context only. If the context doesn't contain the answer, say so.

Always end your response with a line of the exact form:
Confidence: N/5
where N is 1 (no idea) to 5 (definitive).`

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system"`
	Messages  []apiMessage `json:"messages"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Answer submits one question with one context blob and returns the
// recorded usage.
func (a *Anthropic) Answer(ctx context.Context, question, contextBlob string) (Answer, error) {
	body, err := json.Marshal(apiRequest{
		Model: a.ModelName, MaxTokens: a.MaxTokens, System: benchSystemPrompt,
		Messages: []apiMessage{{
			Role:    "user",
			Content: "Context:\n\n" + contextBlob + "\n\nQuestion: " + question,
		}},
	})
	if err != nil {
		return Answer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.APIBase+"/messages", bytes.NewReader(body))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	t0 := time.Now()
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return Answer{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return Answer{}, fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(respBody))
	}
	var parsed apiResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Answer{}, err
	}
	wall := time.Since(t0)
	if len(parsed.Content) == 0 {
		return Answer{Wall: wall, InputTokens: parsed.Usage.InputTokens, OutputTokens: parsed.Usage.OutputTokens}, nil
	}
	text := strings.TrimSpace(parsed.Content[0].Text)
	return Answer{
		Text:         text,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		Wall:         wall,
		Confidence:   parseConfidence(text),
	}, nil
}

var confRe = regexp.MustCompile(`(?i)confidence:\s*(\d)\s*/\s*5`)

func parseConfidence(text string) int {
	m := confRe.FindStringSubmatch(text)
	if len(m) != 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 || n > 5 {
		return 0
	}
	return n
}
