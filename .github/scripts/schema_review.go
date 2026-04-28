// schema_review reads a PR diff, asks the Anthropic API to review it against
// keeba's schema rules, and posts the result as a single PR comment. The
// comment is idempotent — the script edits an existing
// `<!-- keeba-schema-review -->` comment instead of stacking new ones.
//
// The action this script powers (.github/workflows/schema-review.yml) is
// inert when ANTHROPIC_API_KEY is missing. Running locally without it is
// a no-op too.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const marker = "<!-- keeba-schema-review -->"

const systemPrompt = `You are reviewing a pull-request diff against the keeba wiki schema.

Schema rules (from keeba lint):
1. Page must start with YAML frontmatter containing tags, last_verified, and status (one of: current, draft, archived, deprecated, proposed).
2. Page body starts with a single # Title.
3. A '> One-line summary.' must appear within 5 non-empty lines after the title.
4. Page must include '## Sources' and '## See Also' sections.
5. Every [[wiki link]] must resolve to another markdown file in the wiki.
6. Filenames must be lowercase-hyphenated, except SCHEMA.md / README.md / QUERY_PATTERNS.md / dated logs.

Review only the changes in the diff. Be concise: one bullet per concrete issue, file:line where possible. If the diff looks clean, say "no schema issues found." Do not invent rules.
`

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "schema-review: ANTHROPIC_API_KEY missing — skipping")
		return
	}

	base := os.Getenv("BASE_SHA")
	head := os.Getenv("HEAD_SHA")
	repo := os.Getenv("REPO")
	prNum := os.Getenv("PR_NUMBER")
	gh := os.Getenv("GITHUB_TOKEN")
	if base == "" || head == "" || repo == "" || prNum == "" || gh == "" {
		fail("missing PR context env vars")
	}

	diff, err := exec.Command("git", "diff", base+".."+head, "--", "wiki/").Output()
	if err != nil {
		fail("git diff: " + err.Error())
	}
	if len(diff) == 0 {
		fmt.Fprintln(os.Stderr, "schema-review: no wiki/ diff in this PR — nothing to review")
		return
	}

	body, err := callAnthropic(apiKey, string(diff))
	if err != nil {
		fail("anthropic call: " + err.Error())
	}

	out := marker + "\n\n## keeba schema review\n\n" + body + "\n\n_(posted by .github/workflows/schema-review.yml — edit by closing this thread)_\n"
	if err := upsertComment(gh, repo, prNum, out); err != nil {
		fail("post comment: " + err.Error())
	}
}

func callAnthropic(apiKey, diff string) (string, error) {
	payload := map[string]any{
		"model":      "claude-sonnet-4-6",
		"max_tokens": 1024,
		"system":     systemPrompt,
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Review this diff:\n\n```diff\n" + diff + "\n```",
			},
		},
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(b))
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Content) == 0 {
		return "_(empty response)_", nil
	}
	return strings.TrimSpace(parsed.Content[0].Text), nil
}

func upsertComment(gh, repo, prNum, body string) error {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%s/comments?per_page=100", repo, prNum)
	resp, err := ghDo(gh, "GET", listURL, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	var comments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	for _, c := range comments {
		if strings.Contains(c.Body, marker) {
			editURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", repo, c.ID)
			r, err := ghDo(gh, "PATCH", editURL, payload)
			if err != nil {
				return err
			}
			_ = r.Body.Close()
			return nil
		}
	}
	createURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%s/comments", repo, prNum)
	r, err := ghDo(gh, "POST", createURL, payload)
	if err != nil {
		return err
	}
	_ = r.Body.Close()
	return nil
}

func ghDo(token, method, url string, body []byte) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, url, rdr)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := &http.Client{Timeout: 30 * time.Second}
	return c.Do(req)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "schema-review:", msg)
	os.Exit(1)
}
