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

func TestStubsReturnError(t *testing.T) {
	for _, args := range [][]string{
		{"init", "x"},
		{"search", "x"},
		{"ingest", "git"},
		{"bench"},
		{"mcp", "serve"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := NewRoot()
			out := &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(out)
			root.SetArgs(args)
			err := root.Execute()
			if err == nil {
				t.Fatal("expected error from stub")
			}
			if !IsStubError(err) {
				t.Fatalf("expected stub error, got %T %v", err, err)
			}
		})
	}
}
