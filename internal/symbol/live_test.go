package symbol

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveFixture compiles the given files into a fresh repo + .keeba/symbols.json
// and returns a started LiveIndex with its Run() goroutine pumping. Caller
// gets the cancel func and a polling helper that blocks until the supplied
// predicate returns true (used to assert "the watcher saw the change").
func liveFixture(t *testing.T, files map[string]string) (li *LiveIndex, cancel context.CancelFunc, repoRoot string) {
	t.Helper()
	repoRoot = t.TempDir()
	for rel, body := range files {
		full := filepath.Join(repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	if _, err := Compile(repoRoot, repoRoot); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	li, err := NewLiveIndex(repoRoot)
	if err != nil {
		t.Fatalf("NewLiveIndex: %v", err)
	}
	li.flushDur = 50 * time.Millisecond // tighter for tests
	ctx, cancelFn := context.WithCancel(context.Background())
	go func() { _ = li.Run(ctx) }()
	t.Cleanup(func() {
		cancelFn()
		_ = li.Close()
	})
	return li, cancelFn, repoRoot
}

// waitFor polls fn at 25ms intervals until it returns true or the
// timeout elapses, returning whether it converged. Used to test
// fsnotify-driven mutations without flake-prone fixed sleeps.
func waitFor(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fn()
}

func TestLiveIndex_DetectsAddedSymbol(t *testing.T) {
	li, _, repoRoot := liveFixture(t, map[string]string{
		"src/foo.go": "package src\n\nfunc Foo() {}\n",
	})

	// Sanity baseline.
	if got := li.ByName("Foo"); len(got) != 1 {
		t.Fatalf("baseline Foo missing: %v", got)
	}
	if got := li.ByName("Bar"); len(got) != 0 {
		t.Errorf("Bar shouldn't exist yet, got %v", got)
	}

	// Append a new function and wait for the watcher to pick it up.
	full := filepath.Join(repoRoot, "src", "foo.go")
	if err := os.WriteFile(full,
		[]byte("package src\n\nfunc Foo() {}\nfunc Bar() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !waitFor(2*time.Second, func() bool { return len(li.ByName("Bar")) == 1 }) {
		t.Errorf("watcher didn't pick up Bar within 2s")
	}
	// Foo should still be there.
	if got := li.ByName("Foo"); len(got) != 1 {
		t.Errorf("Foo lost after re-extract: %v", got)
	}
}

func TestLiveIndex_DetectsRemovedSymbol(t *testing.T) {
	li, _, repoRoot := liveFixture(t, map[string]string{
		"src/foo.go": "package src\n\nfunc Foo() {}\nfunc Bar() {}\n",
	})

	if got := li.ByName("Bar"); len(got) != 1 {
		t.Fatalf("baseline Bar missing: %v", got)
	}

	// Rewrite the file without Bar.
	full := filepath.Join(repoRoot, "src", "foo.go")
	if err := os.WriteFile(full, []byte("package src\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !waitFor(2*time.Second, func() bool { return len(li.ByName("Bar")) == 0 }) {
		t.Errorf("watcher didn't drop Bar within 2s")
	}
}

func TestLiveIndex_HandlesFileDelete(t *testing.T) {
	li, _, repoRoot := liveFixture(t, map[string]string{
		"src/foo.go":  "package src\n\nfunc Foo() {}\n",
		"src/bar.go":  "package src\n\nfunc Bar() {}\n",
		"src/util.go": "package src\n\nfunc Util() {}\n",
	})

	if got := li.ByName("Bar"); len(got) != 1 {
		t.Fatalf("baseline Bar missing: %v", got)
	}

	full := filepath.Join(repoRoot, "src", "bar.go")
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}

	if !waitFor(2*time.Second, func() bool { return len(li.ByName("Bar")) == 0 }) {
		t.Errorf("watcher didn't drop Bar after file delete within 2s")
	}
	// Adjacent symbols untouched.
	if got := li.ByName("Foo"); len(got) != 1 {
		t.Errorf("Foo collateral damage: %v", got)
	}
	if got := li.ByName("Util"); len(got) != 1 {
		t.Errorf("Util collateral damage: %v", got)
	}
}

func TestLiveIndex_FlushPersistsToDisk(t *testing.T) {
	li, _, repoRoot := liveFixture(t, map[string]string{
		"src/foo.go": "package src\n\nfunc Foo() {}\n",
	})

	// Trigger a write and wait for the in-memory update.
	full := filepath.Join(repoRoot, "src", "foo.go")
	if err := os.WriteFile(full,
		[]byte("package src\n\nfunc Foo() {}\nfunc Bar() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(2*time.Second, func() bool { return len(li.ByName("Bar")) == 1 })

	// Wait at least one flush interval, then re-load from disk.
	time.Sleep(120 * time.Millisecond)
	on, err := Load(repoRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, s := range on.Symbols {
		if s.Name == "Bar" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Bar didn't make it to .keeba/symbols.json after flush; symbols=%v", on.Symbols)
	}
}

func TestLiveIndex_SnapshotIsCopy(t *testing.T) {
	li, _, _ := liveFixture(t, map[string]string{
		"src/foo.go": "package src\n\nfunc Foo() {}\n",
	})

	snap1 := li.Snapshot()
	if snap1 == nil || len(snap1.Symbols) == 0 {
		t.Fatalf("Snapshot empty")
	}

	// Mutate the returned slice.
	snap1.Symbols[0].Name = "MUTATED"

	// Internal state must be untouched.
	matches := li.ByName("Foo")
	if len(matches) != 1 || matches[0].Name == "MUTATED" {
		t.Errorf("Snapshot leaked mutation back into LiveIndex: %v", matches)
	}
}

func TestLiveIndex_MissingIndexErrors(t *testing.T) {
	root := t.TempDir()
	_, err := NewLiveIndex(root)
	if err == nil {
		t.Errorf("expected error when .keeba/symbols.json is missing")
	}
}

func TestWithSlash_NormalizesBackslashes(t *testing.T) {
	if got := withSlash(`a\b\c`); got != "a/b/c" {
		t.Errorf("withSlash = %q, want a/b/c", got)
	}
	if got := withSlash("a/b/c"); got != "a/b/c" {
		t.Errorf("withSlash should be no-op on slash paths, got %q", got)
	}
}

// stripCRLF lets the live test compare bodies on Windows-checked-out
// fixtures without a flake.
func stripCRLF(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

var _ = stripCRLF // referenced to keep helper alongside the live tests
