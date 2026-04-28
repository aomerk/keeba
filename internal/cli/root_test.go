package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	root := NewRoot()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, Version) {
		t.Fatalf("--version output missing %q: %q", Version, got)
	}
}

func TestHelpListsAllSubcommands(t *testing.T) {
	root := NewRoot()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, cmd := range []string{"lint", "drift", "meta", "init", "search", "ingest", "bench", "mcp"} {
		if !strings.Contains(got, cmd) {
			t.Errorf("--help missing %q\n%s", cmd, got)
		}
	}
}

func TestSilentExitSentinel(t *testing.T) {
	if !IsSilentExit(errSilentFail) {
		t.Fatal("errSilentFail should satisfy IsSilentExit")
	}
	if IsSilentExit(nil) {
		t.Fatal("nil should not be a silent exit")
	}
}
