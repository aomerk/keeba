package lint

import (
	"strings"

	"gopkg.in/yaml.v3"
)

const fmDelimiter = "---\n"

// StripFrontmatter returns text with any leading YAML frontmatter removed.
// If there is no frontmatter (or it is unterminated) the text is returned
// unchanged.
func StripFrontmatter(text string) string {
	if !strings.HasPrefix(text, fmDelimiter) {
		return text
	}
	idx := strings.Index(text[len(fmDelimiter):], "\n---\n")
	if idx == -1 {
		return text
	}
	// Skip past the closing "\n---\n".
	return text[len(fmDelimiter)+idx+5:]
}

// ExtractFrontmatter parses the YAML frontmatter at the start of text. If
// there is no frontmatter, parsing fails, or the result is not a map, an
// empty map is returned. The rules layer flags those cases as violations;
// this helper never errors so that callers stay simple.
func ExtractFrontmatter(text string) map[string]any {
	if !strings.HasPrefix(text, fmDelimiter) {
		return map[string]any{}
	}
	idx := strings.Index(text[len(fmDelimiter):], "\n---\n")
	if idx == -1 {
		return map[string]any{}
	}
	raw := text[len(fmDelimiter) : len(fmDelimiter)+idx]
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &node); err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if len(node.Content) == 0 {
		return out
	}
	doc := node.Content[0]
	if doc.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i]
		val := doc.Content[i+1]
		var dst any
		if err := val.Decode(&dst); err != nil {
			continue
		}
		out[key.Value] = dst
	}
	return out
}
