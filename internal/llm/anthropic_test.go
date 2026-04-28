package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseConfidence(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"answer text\nConfidence: 4/5", 4},
		{"long\nlong\nConfidence: 1/5", 1},
		{"confidence:5/5", 5},
		{"no marker", 0},
		{"Confidence: 7/5", 0},
		{"Confidence: 0/5", 0},
	}
	for _, tt := range tests {
		if got := parseConfidence(tt.in); got != tt.want {
			t.Errorf("parseConfidence(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestAnthropicAnswerRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing api key header")
		}
		_ = r.Body.Close()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"text": "It uses JWT.\n\nConfidence: 4/5"}},
			"usage":   map[string]int{"input_tokens": 200, "output_tokens": 25},
		})
	}))
	defer srv.Close()

	a := &Anthropic{
		APIKey: "test-key", APIBase: srv.URL, ModelName: "claude-test",
		HTTPClient: srv.Client(), MaxTokens: 256,
	}
	ans, err := a.Answer(context.Background(), "how does auth work?", "auth uses jwt")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.InputTokens != 200 || ans.OutputTokens != 25 {
		t.Errorf("usage: %+v", ans)
	}
	if ans.Confidence != 4 {
		t.Errorf("confidence: %d", ans.Confidence)
	}
	if ans.Text == "" {
		t.Errorf("empty text")
	}
}

func TestAnthropicAnswerHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()
	a := &Anthropic{APIKey: "k", APIBase: srv.URL, ModelName: "m", HTTPClient: srv.Client(), MaxTokens: 256}
	_, err := a.Answer(context.Background(), "q", "ctx")
	if err == nil {
		t.Fatal("expected error from 429")
	}
}

func TestNewAnthropicReturnsNilWithoutKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	a, err := NewAnthropic()
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	if a != nil {
		t.Fatalf("expected nil client when no key")
	}
}
