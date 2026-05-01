package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallKeebaOutputStyle_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output-styles", "keeba.md")
	changed, err := installKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected change=true on fresh file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("output style not written: %v", err)
	}
	got := string(body)
	// Frontmatter is the contract — Claude Code reads `name` /
	// `description` from here.
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("output style missing frontmatter:\n%s", got)
	}
	if !strings.Contains(got, "name: keeba") {
		t.Errorf("output style missing `name: keeba` in frontmatter:\n%s", got)
	}
}

func TestInstallKeebaOutputStyle_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keeba.md")
	if _, err := installKeebaOutputStyle(path); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	changed, err := installKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("second install should be no-op")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("idempotent install changed bytes")
	}
}

func TestInstallKeebaOutputStyle_OverwritesStale(t *testing.T) {
	// Older version of the style sitting on disk. Re-install must
	// replace it with the current canonical content. Without overwrite,
	// users would silently keep running the old style after upgrading
	// keeba.
	dir := t.TempDir()
	path := filepath.Join(dir, "keeba.md")
	stale := "---\nname: keeba\ndescription: old\n---\n\nold body\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := installKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected change=true when stale content present")
	}
	body, _ := os.ReadFile(path)
	if string(body) != keebaOutputStyle {
		t.Errorf("stale content not replaced — current canonical not written:\n%s", body)
	}
}

func TestActivateKeebaOutputStyle_FreshFile(t *testing.T) {
	// settings.json doesn't exist yet — activation must create it with
	// just the outputStyle key. This is the new-user path.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	changed, err := activateKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected change=true on fresh file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("settings.json not valid JSON: %v", err)
	}
	if got["outputStyle"] != "keeba" {
		t.Errorf("outputStyle field = %v, want \"keeba\"", got["outputStyle"])
	}
}

func TestActivateKeebaOutputStyle_PreservesExistingSettings(t *testing.T) {
	// Real-world settings.json has unrelated keys (theme, hooks,
	// effortLevel, etc.). Activation must merge, not clobber. If we
	// blow away their hooks config, we'd silently break the
	// UserPromptSubmit hook the same install just registered.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{
  "theme": "dark",
  "effortLevel": "high",
  "hooks": {
    "UserPromptSubmit": [{"hooks":[{"type":"command","command":"keeba hook user-prompt-submit"}]}]
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := activateKeebaOutputStyle(path); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("settings.json not valid JSON after activation: %v", err)
	}
	if got["outputStyle"] != "keeba" {
		t.Errorf("outputStyle missing or wrong: %v", got["outputStyle"])
	}
	if got["theme"] != "dark" {
		t.Errorf("theme clobbered: %v", got["theme"])
	}
	if got["effortLevel"] != "high" {
		t.Errorf("effortLevel clobbered: %v", got["effortLevel"])
	}
	if got["hooks"] == nil {
		t.Errorf("hooks dropped — would silently break UserPromptSubmit hook")
	}
}

func TestActivateKeebaOutputStyle_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if _, err := activateKeebaOutputStyle(path); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	changed, err := activateKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Errorf("second activation should be no-op")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("idempotent activation changed bytes")
	}
}

func TestActivateKeebaOutputStyle_OverwritesDifferentStyle(t *testing.T) {
	// User had a different output style set; re-running install with
	// --with-output-style replaces it with keeba. This is the
	// "intentional re-install" path — don't preserve a stale value.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"outputStyle":"verbose","theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := activateKeebaOutputStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected change=true when overwriting different style")
	}
	body, _ := os.ReadFile(path)
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	if got["outputStyle"] != "keeba" {
		t.Errorf("outputStyle = %v, want \"keeba\"", got["outputStyle"])
	}
	if got["theme"] != "dark" {
		t.Errorf("theme dropped during overwrite: %v", got["theme"])
	}
}

func TestKeebaOutputStyle_PinsCriticalPhrases(t *testing.T) {
	// Style content is the actual lever that attacks output-token
	// bloat. Pin the directives so future edits can't quietly soften
	// them. If any of these disappear, the savings claim weakens.
	for _, want := range []string{
		"name: keeba",
		"No preamble",
		"No closing summary",
		"Quote, don't restate",
		"Conclusion first",
		"Forbidden filler",
		"/output-style keeba",
		// Inter-tool silence — the lever that targets the 20-40%
		// slice of investigation output spent on progress markers
		// nobody reads. Pin so future edits can't quietly soften it.
		"Silence between tool calls",
		"Do NOT write transition prose between tool calls",
		"ONE consolidated answer block",
	} {
		if !strings.Contains(keebaOutputStyle, want) {
			t.Errorf("keebaOutputStyle missing phrase %q", want)
		}
	}
}
