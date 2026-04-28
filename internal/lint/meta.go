package lint

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/aomerk/keeba/internal/config"
)

// PageRecord is the per-page entry written to _meta.json.
type PageRecord struct {
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	LastVerified string   `json:"last_verified,omitempty"`
	Status       string   `json:"status"`
	CitedFiles   []string `json:"cited_files"`
}

// MetaIndex is the deserialized shape of _meta.json.
type MetaIndex struct {
	SchemaVersion int          `json:"schema_version"`
	Count         int          `json:"count"`
	Pages         []PageRecord `json:"pages"`
}

// AllPages walks the wiki tree and returns the markdown files that should
// participate in lint, drift, and meta. The skip rules come from
// LintConfig.SkipFilenames + SkipPathParts.
func AllPages(wikiRoot string, lc config.LintConfig) ([]string, error) {
	skipParts := map[string]bool{}
	for _, p := range lc.SkipPathParts {
		skipParts[p] = true
	}
	skipNames := map[string]bool{}
	for _, n := range lc.SkipFilenames {
		skipNames[n] = true
	}
	var out []string
	err := filepath.WalkDir(wikiRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(wikiRoot, path)
		if rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		for _, part := range parts {
			if skipParts[part] {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		if skipNames[d.Name()] {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// PageRecordFor builds the metadata dict for a single page.
func PageRecordFor(path, wikiRoot string, dc config.DriftConfig) (PageRecord, error) {
	body, err := readFile(path)
	if err != nil {
		return PageRecord{}, fmt.Errorf("read %s: %w", path, err)
	}
	rel, err := filepath.Rel(wikiRoot, path)
	if err != nil {
		return PageRecord{}, fmt.Errorf("rel %s vs %s: %w", path, wikiRoot, err)
	}
	rel = filepath.ToSlash(rel)
	slug := strings.TrimSuffix(rel, filepath.Ext(rel))
	parts := strings.SplitN(slug, "/", 2)
	category := "_root"
	if len(parts) > 1 {
		category = parts[0]
	}

	title := extractTitle(body)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	fm := ExtractFrontmatter(body)

	tags := stringList(fm["tags"])

	status, _ := fm["status"].(string)
	if status == "" {
		status = "unknown"
	}

	lastVerified := ""
	if v, ok := fm["last_verified"]; ok {
		switch t := v.(type) {
		case time.Time:
			lastVerified = t.Format("2006-01-02")
		case string:
			lastVerified = t
		default:
			lastVerified = fmt.Sprintf("%v", v)
		}
	}

	var cited []string
	if list := stringList(fm["cited_files"]); len(list) > 0 {
		cited = list
	} else {
		seen := map[string]bool{}
		for _, c := range ExtractCitations(StripFrontmatter(body), path, dc) {
			if !seen[c.RepoPath] {
				cited = append(cited, c.RepoPath)
				seen[c.RepoPath] = true
			}
		}
	}
	if cited == nil {
		cited = []string{}
	}

	if tags == nil {
		tags = []string{}
	}

	return PageRecord{
		Slug:         slug,
		Title:        title,
		Category:     category,
		Tags:         tags,
		LastVerified: lastVerified,
		Status:       status,
		CitedFiles:   cited,
	}, nil
}

// BuildMeta walks the wiki and returns a MetaIndex sorted by slug.
func BuildMeta(wikiRoot string, lc config.LintConfig, dc config.DriftConfig) (MetaIndex, error) {
	pages, err := AllPages(wikiRoot, lc)
	if err != nil {
		return MetaIndex{}, err
	}
	records := make([]PageRecord, 0, len(pages))
	for _, p := range pages {
		rec, err := PageRecordFor(p, wikiRoot, dc)
		if err != nil {
			return MetaIndex{}, err
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Slug < records[j].Slug
	})
	return MetaIndex{SchemaVersion: 1, Count: len(records), Pages: records}, nil
}

// Marshal returns the deterministic JSON serialization (2-space indent +
// trailing newline) used for _meta.json.
func Marshal(index MetaIndex) ([]byte, error) {
	b, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	return b, nil
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	case []string:
		return slices.Clone(t)
	default:
		return nil
	}
}
