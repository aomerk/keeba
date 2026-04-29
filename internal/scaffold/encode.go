package scaffold

import (
	"fmt"

	"github.com/aomerk/keeba/internal/config"
	"github.com/aomerk/keeba/internal/encoding"
)

// encodedBody is the result of applying the page-type-aware encoding
// pipeline to a wiki page body. PageType and Pipeline get persisted in
// frontmatter so subsequent `keeba sync` runs can reproduce the encoding.
type encodedBody struct {
	Body     string
	PageType encoding.PageType
	Pipeline string // empty when no encoding was applied
}

// applyEncoding detects the page-type from body + cited_files and runs
// the matching pipeline from cfg. Returns the original body unchanged
// when no pipeline is configured for the type, or the cfg is empty, or
// the pipeline build fails (graceful degradation — encoding is a
// nice-to-have, not a blocker for ingest).
//
// Glossary-style stateful encoders are effectively no-ops here because
// the corpus seen at ingest time is just one page. The bench-time grid
// (`keeba bench --encoding-grid-by-type`) is the right tool for
// glossary-relevant compression; ingest-time encoding focuses on the
// stateless plugins (caveman, structural-card, dense-tuple).
func applyEncoding(body string, citedFiles []string, enc config.EncodingConfig) encodedBody {
	pageType := encoding.DetectPageType(body, citedFiles)
	spec := enc.PipelineForType(string(pageType))
	if spec == "" {
		return encodedBody{Body: body, PageType: pageType}
	}
	pipeline, err := encoding.BuildPipeline(spec)
	if err != nil {
		// Bad config shouldn't break ingest. Warn via the result-shape
		// (Pipeline stays empty so frontmatter stays clean) and return
		// the original body.
		return encodedBody{Body: body, PageType: pageType}
	}
	encodedText, err := pipeline.Encode(body)
	if err != nil {
		return encodedBody{Body: body, PageType: pageType}
	}
	return encodedBody{
		Body:     encodedText,
		PageType: pageType,
		Pipeline: pipeline.Name(),
	}
}

// citedFileFromOrigin builds the conventional "<repo>/<origin-path>"
// citation string used by wrapImported's frontmatter. The Encoding
// detector inspects the file extension to pick function vs narrative.
func citedFileFromOrigin(repoName, origin string) string {
	return fmt.Sprintf("%s/%s", repoName, origin)
}
