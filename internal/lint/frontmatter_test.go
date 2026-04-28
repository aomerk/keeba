package lint

import (
	"reflect"
	"testing"
)

func TestStripFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no frontmatter", "# Page\n\nbody\n", "# Page\n\nbody\n"},
		{"frontmatter present", "---\ntags: [a]\n---\n# Page\n", "# Page\n"},
		{
			"frontmatter unterminated",
			"---\ntags: [a]\n# Page\n",
			"---\ntags: [a]\n# Page\n",
		},
		{"empty body after frontmatter", "---\nfoo: bar\n---\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripFrontmatter(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]any
	}{
		{"missing", "# Page\n", map[string]any{}},
		{"empty", "---\n---\n# Page\n", map[string]any{}},
		{"unterminated", "---\nfoo: bar\n# Page\n", map[string]any{}},
		{
			"present",
			"---\ntags: [a, b]\nstatus: current\n---\n# Page\n",
			map[string]any{"tags": []any{"a", "b"}, "status": "current"},
		},
		{
			"malformed",
			"---\nthis: is: not: valid\n---\n# Page\n",
			map[string]any{},
		},
		{
			"non-map",
			"---\n- just\n- a\n- list\n---\n# Page\n",
			map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFrontmatter(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
