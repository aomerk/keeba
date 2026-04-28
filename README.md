# keeba

> Bootstrap an AI-native wiki in five minutes: schema discipline, drift detection, MCP integration, ingest agents.

Pronounced "kee-bah" — KB spelled phonetically.

## Install

```bash
go install github.com/aomerk/keeba/cmd/keeba@latest
keeba --version
```

A Homebrew tap lands at v0.2.

## 60-second tour

```bash
# 1. scaffold
keeba init my-wiki && cd my-wiki

# 2. write a page (or let an ingest agent do it)
$EDITOR concepts/auth.md

# 3. enforce conventions
keeba lint                # schema rules: frontmatter, title, summary, wikilinks, …
keeba drift               # verify backtick code citations are still in bounds
keeba meta                # rebuild _meta.json + _xref/ for token-cheap agent reads

# 4. find things
keeba search "JWT tokens" # BM25 over the wiki
keeba bench --raw ../src  # measure how much keeba reduces the read budget

# 5. plug it into Claude Code / Cursor / Codex
#    .mcp.json was scaffolded for you — `keeba mcp serve` is wired automatically
```

## What v0.1 ships

| Command | What it does |
|---|---|
| `keeba init [name]` | Scaffolds a wiki repo: SCHEMA, index, log, agents/, lint + meta workflows, `.mcp.json`, seed page. |
| `keeba lint` | Schema rules — title, summary, sources, see-also, wikilinks, filename casing, frontmatter. |
| `keeba drift` | Citation drift — every backtick `repo/path:line` cite must point at a real file in bounds. |
| `keeba meta` | Builds `_meta.json` + `_xref/<repo>.json`. `--check` mode for CI. |
| `keeba search QUERY` | BM25 keyword search (pure Go, no embeddings). Top-k pages with title, score, snippet. |
| `keeba ingest git\|slack` | Drops a tested agent prompt template into `agents/` for your AI tool to run on cron. |
| `keeba bench` | Runs the default 5-question code-project bench, writes `_bench/<date>.md`, prints the headline ratio. |
| `keeba mcp serve` | Stdio MCP server. One tool: `query_documentation`. Claude Code, Cursor, Codex, Cline all speak this. |

## What v0.2 adds

- Vector search — sqlite-vec + local sentence-transformers as default provider; Voyage AI + OpenAI as opt-in.
- Direct execution of the ingest templates from inside `keeba ingest` (rather than handing them to an external runner).
- Cursor + Codex tool-config scaffolding alongside `.mcp.json`.
- `keeba bench --diff` for tracking ratio drift over time.
- Homebrew tap.

## Why this exists

Every team eventually rebuilds the same thing: schema-clean wiki + ingest
agents + drift detection + MCP integration. keeba is that, productized.
One command to bootstrap, opinionated defaults, AI-tool agnostic via MCP.

The thesis: there's a hole between "DIY with LangChain" (3-week project)
and "Notion subscription" (vendor lock). keeba lives in it.

## Configuration

`keeba.config.yaml` at the wiki root drives everything. Sensible defaults
mean v0.1 commands work even without the file.

```yaml
schema_version: 1
name: "my-wiki"
purpose: "Knowledge base for the foo team."
lint:
  required_frontmatter_fields: [tags, last_verified, status]
  valid_status_values: [current, draft, archived, deprecated, proposed]
drift:
  # Add repos you cite from. Without prefixes, drift never flags.
  repo_prefixes: ["my-app/", "my-infra/"]
  gigarepo_root: ".."
```

## Examples

- [`examples/llm-c/`](examples/llm-c/README.md) — recipe for bootstrapping a
  wiki on Karpathy's `llm.c`.

## Bench number

From the bundled `dogfood.sh` against a freshly-scaffolded wiki:

```
keeba: 126.8× cheaper, 27.5× faster (5 questions)
```

That's a tiny corpus, so the absolute number is mostly a sanity check —
the headline ratios trend more usefully as the wiki accumulates pages.
Track yours by checking `_bench/<date>.md` into git.

## Status

Pre-alpha. v0.1 is the public skeleton + working CLI. Roadmap and bug
tracker on [GitHub Issues](https://github.com/aomerk/keeba/issues).

## License

[MIT](LICENSE).
