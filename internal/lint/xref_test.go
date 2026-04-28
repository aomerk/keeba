package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aomerk/keeba/internal/config"
)

func xrefDriftCfg() config.DriftConfig {
	return config.DriftConfig{RepoPrefixes: []string{"my-app/", "my-infra/"}}
}

func TestBuildXrefGroupsByRepo(t *testing.T) {
	pages := []PageRecord{
		{Slug: "concepts/parity", Title: "Parity", CitedFiles: []string{"my-app/pkg/types.go", "my-infra/main.tf"}},
		{Slug: "concepts/schema", Title: "Schema", CitedFiles: []string{"my-app/pkg/types.go"}},
		{Slug: "concepts/orphan", Title: "Orphan"},
	}
	got := BuildXref(pages, xrefDriftCfg())
	if got["my-app"] == nil || got["my-infra"] == nil {
		t.Fatalf("missing repos: %+v", got)
	}
	citers := got["my-app"]["pkg/types.go"]
	if len(citers) != 2 {
		t.Fatalf("citers: %v", citers)
	}
	want := []XrefEntry{{Slug: "concepts/parity", Title: "Parity"}, {Slug: "concepts/schema", Title: "Schema"}}
	if !reflect.DeepEqual(citers, want) {
		t.Fatalf("got %v want %v", citers, want)
	}
}

func TestBuildXrefStripsLineSuffix(t *testing.T) {
	pages := []PageRecord{
		{Slug: "p1", Title: "P1", CitedFiles: []string{"my-app/pkg/foo.go:123"}},
		{Slug: "p2", Title: "P2", CitedFiles: []string{"my-app/pkg/foo.go:456"}},
	}
	got := BuildXref(pages, xrefDriftCfg())
	if len(got["my-app"]["pkg/foo.go"]) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestBuildXrefSkipsUnknownRepo(t *testing.T) {
	pages := []PageRecord{{Slug: "p", Title: "P", CitedFiles: []string{"random/path/foo.go"}}}
	got := BuildXref(pages, xrefDriftCfg())
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestBuildXrefEmpty(t *testing.T) {
	if got := BuildXref(nil, xrefDriftCfg()); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestWriteXrefPerRepo(t *testing.T) {
	root := t.TempDir()
	pages := []PageRecord{
		{Slug: "concepts/parity", Title: "Parity", CitedFiles: []string{"my-app/pkg/types.go"}},
	}
	xref := BuildXref(pages, xrefDriftCfg())
	if _, err := WriteXref(xref, root); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "_xref", "my-app.json")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed map[string][]map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed["pkg/types.go"]) != 1 {
		t.Fatalf("payload: %v", parsed)
	}
}

func TestWriteXrefRemovesStaleFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "_xref")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	pages := []PageRecord{{Slug: "p", Title: "P", CitedFiles: []string{"my-app/pkg/foo.go"}}}
	if _, err := WriteXref(BuildXref(pages, xrefDriftCfg()), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.json")); !os.IsNotExist(err) {
		t.Fatalf("stale.json not removed (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "my-app.json")); err != nil {
		t.Fatalf("my-app.json missing: %v", err)
	}
}
