---
tags: [decision, github-ingest]
last_verified: 2026-04-28
status: current
pr_number: 2
---

# feat: complete v0.1 — init, search, bench, ingest, mcp serve all wired

> Auto-imported from PR #2. Edit to match the real ADR shape.

## Context

## What ships

Closes the v0.1 build sequence (steps 3, 5, 6, 7, 8, 9, 10 of \`keeba-vision.md §17\`). Every command in the help text now has real implementation. Nothing is stubbed.

## New commands

| Command | Implementation |
|---|---|
| \`keeba init [name]\` | Scaffolds a fresh wiki from \`embed.FS\` templates: SCHEMA, index, log, agents/, .github/workflows/, .mcp.json, keeba.config.yaml, .gitignore, a seed concept page, and four category dirs. \`--force\` overrides non-empty target. |
| \`keeba search QUERY\` | Pure-Go BM25 keyword search. No embeddings, no FFI, no API keys. Top-k with title, score, snippet. \`--format json\` for tooling. |
| \`keeba bench\` | Wiki-vs-raw token-count benchmark. 5 default code-project questions (override with \`--questions\`). Writes \`_bench/<date>.md\`, prints headline ratio. |
| \`keeba mcp serve\` | Minimal stdio JSON-RPC MCP server (protocol 2024-11-05). One tool: \`query_documentation\`. Supports initialize / tools/list / tools/call. Wired automatically by \`keeba init\` via \`.mcp.json\`. |
| \`keeba ingest git\\|slack\` | Drops a tested agent prompt template into \`agents/\` for the user's AI tool to run on cron. \`--dry-run\` prints to stdout. |

## Bench number from the dogfood

\`\`\`
keeba: 126.8× cheaper, 27.5× faster (5 questions)
\`\`\`

Tiny corpus, so the absolute number is mostly a sanity signal — the ratio gets meaningful as the wiki grows.

## Decision: BM25 in v0.1, vectors in v0.2

The locked plan said \"local sentence-transformers as the default embedding\". Pure-Go sentence-transformers is non-trivial (cybertron + ONNX runtime + model download), so v0.1 ships BM25 to avoid that dependency at launch. The locked decision still holds for v0.2 when vectors actually land. README explains.

## Verification

- \`go build ./...\` — exit 0
- \`go test ./...\` — 7 packages, ~80 cases, all green with \`-race\`
  - bench, cli, config, lint, mcp, scaffold, search
- \`golangci-lint run ./...\` — 0 issues
- \`./dogfood.sh\` — runs the full surface against a freshly-scaffolded wiki:
  - init / lint / drift / meta / meta --check / search / bench / ingest / mcp serve
  - All exit 0, mcp returns a valid \`protocolVersion\` over stdio

## Layout deltas vs PR #1

\`\`\`
+ internal/scaffold/         init templates + writer (embed.FS)
+ internal/search/           BM25 index + tokenizer
+ internal/mcp/              stdio JSON-RPC server
+ internal/bench/            wiki-vs-raw benchmark + markdown formatter
+ internal/cli/init.go       wires scaffold pkg
+ internal/cli/search.go     wires BM25
+ internal/cli/mcp.go        wires MCP server
+ internal/cli/bench.go      wires bench
+ internal/cli/ingest.go     embeds + writes ingest templates
+ internal/cli/sentinels.go  silentExit (replaces stubs.go)
+ examples/llm-c/README.md   worked example recipe
- internal/cli/stubs.go      no longer needed; all commands real
\`\`\`

## What's deliberately out of scope

- Vector search (v0.2 — adds the embedding provider plumbing).
- Direct execution of ingest agents (v0.2 — until then \`keeba ingest\` only writes the prompt template).
- Cursor + Codex tool-config scaffolding (v0.2; \`.mcp.json\` covers Claude Code in v0.1).
- 30-second screencast — needs a human terminal session.

## Test plan for reviewers

\`\`\`bash
git fetch && git checkout v0.1-complete
go build ./... && go test ./... -race && golangci-lint run ./...
./dogfood.sh
\`\`\`

🤖 Generated with [Claude Code](https://claude.com/claude-code)

## Sources

- pr: https://github.com/aomerk/keeba/pull/2
- merged: 2026-04-28T08:57:13Z
- author: @aomerk

## See Also

- [[log]]
