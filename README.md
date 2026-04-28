# keeba

> Bootstrap an AI-native wiki: schema discipline, drift detection, MCP integration, ingest agents. Pre-alpha.

Pronounced "kee-bah" — KB spelled phonetically.

## Install

```bash
go install github.com/aomerk/keeba/cmd/keeba@latest
keeba --version
```

A Homebrew tap lands at v0.2.

## What v0.1 ships

- `keeba lint` — schema rules over a wiki: title, summary, sources, see-also, wikilinks, filename casing, frontmatter required fields. Text or JSON output.
- `keeba drift` — citation drift: every backtick reference to a file in a configured repo prefix gets verified against disk (file exists, lines in bounds).
- `keeba meta` — builds `_meta.json` (per-page title/tags/last_verified/cited_files) and `_xref/<repo>.json` (reverse index, code-path → citing pages). `--check` mode for CI.
- `keeba init / search / ingest / bench / mcp serve` — declared and stubbed for follow-up PRs (each prints "not yet implemented" and exits 2).

## What v0.2 adds

- `keeba init <name>` — scaffolds a fresh wiki with the schema, lint, frontmatter, sample agents, and `.mcp.json` wired for Claude Code.
- `keeba search` — sqlite-vec semantic search, defaulting to local sentence-transformers (no API key required); Voyage AI + OpenAI as opt-in providers.
- `keeba ingest git` and `keeba ingest slack` — semantic-digest ingest agents.
- `keeba bench` — wiki-vs-raw-source benchmark with shipped question sets.
- `keeba mcp serve` — stdio MCP server exposing `query_documentation`.

## Why this exists

Every team eventually rebuilds the same thing: schema-clean wiki + ingest agents + drift detection + MCP integration. keeba is that, productized. One command to bootstrap, opinionated defaults, AI-tool agnostic via MCP.

## Configuration

`keeba.config.yaml` at the wiki root drives everything. v0.1 commands fall back to sane defaults if the file is absent.

```yaml
schema_version: 1
name: "my-wiki"
purpose: "Knowledge base for the foo team."
lint:
  required_frontmatter_fields: [tags, last_verified, status]
  valid_status_values: [current, draft, archived, deprecated, proposed]
drift:
  repo_prefixes: ["my-app/", "my-infra/"]
  gigarepo_root: ".."
```

## Status

Pre-alpha. The skeleton lands with this PR; the v0.1 build sequence is tracked in [GitHub Issues](https://github.com/aomerk/keeba/issues).

## License

[MIT](LICENSE).
