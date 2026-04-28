package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aomerk/keeba/internal/config"
)

// XrefEntry is one citing-page line in the reverse index.
type XrefEntry struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Xref maps repo → in-repo path → list of citing pages.
type Xref map[string]map[string][]XrefEntry

// BuildXref turns a list of meta-index page records into a per-repo reverse
// index. Citations whose path doesn't match any configured prefix are
// skipped.
func BuildXref(pages []PageRecord, dc config.DriftConfig) Xref {
	out := Xref{}
	for _, p := range pages {
		for _, cited := range p.CitedFiles {
			repo, in := splitByPrefix(cited, dc.RepoPrefixes)
			if repo == "" {
				continue
			}
			if _, ok := out[repo]; !ok {
				out[repo] = map[string][]XrefEntry{}
			}
			out[repo][in] = append(out[repo][in], XrefEntry{Slug: p.Slug, Title: p.Title})
		}
	}
	for repo := range out {
		for path := range out[repo] {
			sort.Slice(out[repo][path], func(i, j int) bool {
				return out[repo][path][i].Slug < out[repo][path][j].Slug
			})
		}
	}
	return out
}

// WriteXref serializes the reverse index to wiki/_xref/<repo>.json files,
// removing files that no longer have any citations.
func WriteXref(x Xref, wikiRoot string) (int, error) {
	dir := filepath.Join(wikiRoot, "_xref")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	expected := map[string]bool{}
	for repo, paths := range x {
		ordered := make([]string, 0, len(paths))
		for k := range paths {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		serializable := make([][2]any, 0, len(ordered))
		for _, k := range ordered {
			serializable = append(serializable, [2]any{k, paths[k]})
		}
		// gopls won't let us have ordered keys via json. Build manually.
		var sb strings.Builder
		sb.WriteString("{\n")
		for i, k := range ordered {
			b, err := json.MarshalIndent(paths[k], "  ", "  ")
			if err != nil {
				return 0, err
			}
			fmt.Fprintf(&sb, "  %q: %s", k, string(b))
			if i < len(ordered)-1 {
				sb.WriteByte(',')
			}
			sb.WriteByte('\n')
		}
		sb.WriteString("}\n")
		out := filepath.Join(dir, repo+".json")
		if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
			return 0, fmt.Errorf("write %s: %w", out, err)
		}
		expected[repo+".json"] = true
		_ = serializable
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return len(x), nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if !expected[e.Name()] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return len(x), nil
}

func splitByPrefix(cited string, prefixes []string) (repo, inRepo string) {
	pathOnly, _, _ := strings.Cut(cited, ":")
	for _, p := range prefixes {
		if strings.HasPrefix(pathOnly, p) {
			return strings.TrimSuffix(p, "/"), pathOnly[len(p):]
		}
	}
	return "", ""
}
