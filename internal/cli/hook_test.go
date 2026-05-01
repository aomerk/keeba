package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aomerk/keeba/internal/symbol"
)

// hookFixtureRepo writes a tiny Go corpus + compiles its symbol graph,
// returns the repo dir. Same shape as the context package's fixtureRepo
// but locally scoped so this test file is self-contained.
func hookFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"src/auth.go": `package src

// AuthMiddleware validates JWT tokens before passing to next handler.
func AuthMiddleware() string {
	return "ok"
}
`,
		"src/billing.go": `package src

// BillingHandler charges the customer via Stripe.
func BillingHandler() string {
	return "billed"
}
`,
	}
	for path, body := range files {
		full := filepath.Join(repo, path)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte(body), 0o644)
	}
	if _, err := symbol.Compile(repo, repo); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return repo
}

// runHookOnce drives runUserPromptSubmit with a synthesized stdin
// payload and returns the parsed output JSON (or empty if invalid).
func runHookOnce(t *testing.T, prompt, cwd string) userPromptSubmitOutput {
	t.Helper()
	in, _ := json.Marshal(userPromptSubmitInput{Prompt: prompt, CWD: cwd})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runUserPromptSubmit(bytes.NewReader(in), stdout, stderr, false); err != nil {
		t.Fatalf("runUserPromptSubmit: %v", err)
	}
	var out userPromptSubmitOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("parse stdout: %v\nraw: %s", err, stdout.String())
	}
	return out
}

func TestUserPromptSubmit_InjectsContextWhenHits(t *testing.T) {
	repo := hookFixtureRepo(t)
	out := runHookOnce(t, "Investigate AuthMiddleware token validation flow", repo)
	if out.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Errorf("hookEventName=%q want UserPromptSubmit", out.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "AuthMiddleware") {
		t.Errorf("additionalContext missing AuthMiddleware:\n%s", out.HookSpecificOutput.AdditionalContext)
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "# keeba context") {
		t.Errorf("missing markdown headline:\n%s", out.HookSpecificOutput.AdditionalContext)
	}
}

func TestUserPromptSubmit_EmptyContextWhenNoHits(t *testing.T) {
	repo := hookFixtureRepo(t)
	// Prompt with no code-shaped tokens, no quoted literals, no
	// terms that match symbol name/sig/doc.
	out := runHookOnce(t, "what is the weather today", repo)
	if out.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Errorf("hookEventName mismatch")
	}
	if out.HookSpecificOutput.AdditionalContext != "" {
		t.Errorf("expected empty additionalContext for non-code prompt, got:\n%s",
			out.HookSpecificOutput.AdditionalContext)
	}
}

func TestUserPromptSubmit_EmptyContextOnNoSymbolGraph(t *testing.T) {
	// Repo without .keeba/symbols.json — Build() returns an error,
	// hook should swallow and emit empty.
	repo := t.TempDir()
	out := runHookOnce(t, "investigate AuthMiddleware", repo)
	if out.HookSpecificOutput.AdditionalContext != "" {
		t.Errorf("expected empty additionalContext when no symbol graph, got:\n%s",
			out.HookSpecificOutput.AdditionalContext)
	}
}

func TestUserPromptSubmit_EmptyOnGarbageStdin(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runUserPromptSubmit(strings.NewReader("not json at all"), stdout, stderr, false); err != nil {
		t.Fatalf("expected nil error on garbage stdin, got %v", err)
	}
	var out userPromptSubmitOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode empty response: %v", err)
	}
	if out.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Errorf("expected event name preserved on empty path")
	}
	if out.HookSpecificOutput.AdditionalContext != "" {
		t.Errorf("expected empty context on bad input")
	}
}

func TestUserPromptSubmit_EmptyOnEmptyPrompt(t *testing.T) {
	repo := hookFixtureRepo(t)
	out := runHookOnce(t, "   ", repo)
	if out.HookSpecificOutput.AdditionalContext != "" {
		t.Errorf("expected empty context for whitespace-only prompt, got:\n%s",
			out.HookSpecificOutput.AdditionalContext)
	}
}

func TestUserPromptSubmit_RespectsHookOutputCap(t *testing.T) {
	repo := hookFixtureRepo(t)
	// A hit-rich prompt should still come back under hookOutputCap.
	out := runHookOnce(t, "Investigate AuthMiddleware and BillingHandler stripe customer charge", repo)
	if len(out.HookSpecificOutput.AdditionalContext) > hookOutputCap+200 {
		t.Errorf("additionalContext=%d bytes, exceeds cap+slop", len(out.HookSpecificOutput.AdditionalContext))
	}
}
