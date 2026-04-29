package symbol

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx := Index{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
		RepoRoot:      "/tmp/somewhere",
		NumFiles:      2,
		NumSymbols:    1,
		Symbols: []Symbol{
			{
				Name:      "foo",
				Kind:      "function",
				File:      "src/foo.go",
				StartLine: 10,
				EndLine:   20,
				Signature: "func foo() error",
				Language:  "go",
			},
		},
	}

	if err := Save(dir, idx); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.NumSymbols != 1 || got.Symbols[0].Name != "foo" || got.Symbols[0].StartLine != 10 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestLoadMissingReturnsError(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error from missing index")
	}
}

func TestCompileEndToEnd(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "foo.go"),
		[]byte(`package src

func Foo() {}
func Bar() {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "user.py"),
		[]byte(`class User:
    def greet(self):
        pass
`), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := Compile(repo, repo)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if idx.NumSymbols < 4 {
		t.Errorf("expected at least 4 symbols, got %d", idx.NumSymbols)
	}
	byLang := idx.CountByLanguage()
	if byLang["go"] < 2 {
		t.Errorf("go count = %d, want >= 2", byLang["go"])
	}
	if byLang["py"] < 2 {
		t.Errorf("py count = %d, want >= 2", byLang["py"])
	}
}

func TestExtractRepoSkipsHiddenAndVendoredDirs(t *testing.T) {
	repo := t.TempDir()
	for _, dir := range []string{".git", "node_modules", "vendor", ".venv"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, dir, "junk.go"),
			[]byte(`package x; func ShouldNotAppear() {}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "real.go"),
		[]byte(`package x; func RealFunc() {}`), 0o644); err != nil {
		t.Fatal(err)
	}

	syms, err := ExtractRepo(repo)
	if err != nil {
		t.Fatalf("ExtractRepo: %v", err)
	}
	for _, s := range syms {
		if s.Name == "ShouldNotAppear" {
			t.Errorf("symbol from skipped dir leaked: %+v", s)
		}
	}
	found := false
	for _, s := range syms {
		if s.Name == "RealFunc" {
			found = true
		}
	}
	if !found {
		t.Errorf("real symbol RealFunc not extracted; got %v", syms)
	}
}
