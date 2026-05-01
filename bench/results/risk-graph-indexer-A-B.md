# keeba A/B — `risk-graph-indexer` (real Claude Code session)

_2026-05-01_

This is the receipt that anchors keeba's headline. Synthetic per-tool bench
numbers (see [`go-ethereum.md`](go-ethereum.md)) are real but only measure
returned-vs-alternative bytes for the tools themselves. The **per-session
Claude cost** is what the user actually pays — Claude's reasoning, output,
system prompt, and MCP schemas all sit outside the alternative-bytes math.

So we ran the real thing.

## Setup

- **Repo**: `risk-graph-indexer` (Forta's realtime Ethereum indexer, Go 1.25)
- **Compiled**: 3,962 symbols across 371 files, ~1s
- **Tool**: Claude Code v2.1.126, Opus 4.7 (1M context), xhigh effort
- **Prompt**: a real Slack-thread investigation — Carlos claims the indexer
  duplicates `HOLDS` edges and misses dormant whales (WTGXX example);
  ali counter-claims the initial Blockscout scan should catch all WTGXX
  holders since there are few of them. Agent must verify or refute against
  source code with file:line citations.

Two arms, same prompt, same Claude Code build:

- **keeba arm**: `keeba mcp install --tool claude-code --patch-agents
  --with-claude-md` applied first; main session has the keeba MCP tools and
  CLAUDE.md guidance to prefer them. Explicit "use keeba" prompt nudge.
- **no-keeba arm**: `--strict-mcp-config /dev/null` flag at launch — global
  MCP servers off, project `.mcp.json` still active (symmetric noise).
  Explicit "do not use keeba" prompt nudge.

## Result

| | keeba arm | no-keeba arm | Δ |
|---|---|---|---|
| **Cost** | **$1.45** | **$2.19** | **−34% (saved $0.74)** |
| **Cache read** | **693k tokens** | **1.10M tokens** | **−37% (407k tokens)** |
| Cache write | 135k | 210k | −36% |
| Output | 10.3k | 12.4k | similar |
| API time | 2m 47s | 3m 38s | keeba faster |
| Wall time | 5m 25s | 4m 16s | keeba slower (more roundtrips) |
| Quality | 2 bugs found, file:line cites | 2 bugs found, file:line cites | parity |

**Headline: ~30% cheaper per session, with quality parity.**

Both arms found the same two bugs at the same file:line citations:
1. Inconsistent `MERGE` keys for `HOLDS` edges across writers
   (`pkg/enrichment/holder_seeder.go:420`,
   `pkg/indexer/handlers_protocol.go:912`,
   `pkg/indexer/batch_writer.go:673`, others) → duplicate edges per
   `(token, holder)` pair.
2. Boot-once seeder gate (`cmd/enrichment-worker/main.go:925`) plus sticky
   per-token completion key
   (`pkg/enrichment/holder_seeder.go:183,198`) → tokens added to focus
   after first boot never get the bootstrap sweep; partial Blockscout
   responses still mark complete.

The keeba arm called `mcp__keeba__*` 16 times in a single investigation;
the no-keeba arm did 8+ `Bash(rg ...)` and 6+ `Read(...)` calls of
full files.

## Honest caveats

- **Single A/B, single prompt.** This is one investigation, not a meta-analysis.
  The 30% number is the order-of-magnitude — your result will vary
  ±10% depending on prompt shape (impact-tracing prompts benefit most;
  "explain this concept" prompts barely move).
- **Quality parity isn't free.** Both arms still needed an explicit prompt
  nudge ("use keeba" / "do not use keeba"). Without the nudge, even the
  keeba-equipped session sometimes fell back to Read/Grep out of training
  habit. `keeba mcp install --with-claude-md` adds CLAUDE.md guidance to
  steer Claude toward keeba; the assertiveness of that guidance is
  iterating.
- **Wall time was slightly slower** for keeba (5m25 vs 4m16) — more tool
  roundtrips, smaller payloads each. API time was faster (2m47 vs 3m38)
  because the agent spent less time reasoning over big context blobs.
- **Cache_read is the real lever.** Saving 407k tokens of cache_read per
  investigation = where the dollar savings come from. Output tokens stayed
  similar because the answer is the answer regardless of how it was found.

## Reproduce

```bash
go install github.com/aomerk/keeba/cmd/keeba@latest
cd /path/to/your-go-repo
keeba compile .

# Wire MCP + apply the install patches
keeba mcp install --tool claude-code --patch-agents --with-claude-md

# Restart Claude Code, then in two terminals:
#   A: claude                                  # keeba arm
#   B: claude --strict-mcp-config /dev/null    # no-keeba arm
# Same prompt to both. /cost after each.
```

A typical impact-tracing prompt (find callers + check tests + verify behavior)
is the right shape. Conceptual / write-from-scratch prompts won't move the
needle — keeba's leverage is in code lookup, not generation.
