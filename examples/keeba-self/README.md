# Example: keeba ingesting its own PR history

> Real output from running `keeba ingest github --execute` against this repo. The pages in [`decisions/`](decisions/) were generated, not hand-written.

## Why this is the demo

Your AI tool can read your code. It cannot read **why** the code looks the way it does — that lives in PR descriptions. Run keeba once, ask Claude Code "why did keeba ship BM25 in v0.1 instead of vector search?" and you get a real answer rooted in the PR #2 body, not a hallucination.

## Recipe (reproducible)

```bash
go install github.com/aomerk/keeba/cmd/keeba@latest

keeba init keeba-self --purpose "Wiki digesting keeba's own PR history"
cd keeba-self

# scan all merged PRs in the last 90 days, classify them, write
# decisions/<slug>.md for the ones that carry durable signal
keeba ingest github --execute --github-repo aomerk/keeba --since 90d
```

## What gets caught

Two of keeba's PRs are detected as decisions because their bodies carry structured rationale:

- **PR #2** — `feat: complete v0.1` — has a `## Decision` section explaining why v0.1 ships BM25 instead of vectors.
- **PR #7** — `feat: keeba sync --from-repo` — has `## Why this matters` explaining the wiki-rot failure mode keeba is solving.

Five other merged PRs (#1, #3, #4, #5, #6) are correctly classified as noise: they're feature/fix PRs without ADR-shaped bodies.

## What the heuristic looks for

Highest signal first:

1. **PR labels** — `architecture`, `decision`, `adr`, `breaking`, `incident`, `post-mortem` (case-insensitive).
2. **Title prefixes** — Conventional Commits `BREAKING:` or `feat!:`.
3. **Title keywords** — `incident`, `outage`, `RCA`, `hotfix`, `architecture`, `ADR`.
4. **Body section headings** — `## Decision`, `## Trade-offs`, `## Why this matters`, `## What happened`, `## Breaking changes`, `## Migration`.
5. **Major-version dep bumps** in the title.

Body section headings are the v0.4 addition that catches the common case of a `feat:` PR with a real decision write-up in its body.

## Idempotency

Each output page records `pr_number: <n>` in frontmatter. Re-running the same command:

```
wrote 0 action(s); skipped 2 already-imported; 5 noise.
```

So you can put this in a daily cron and never worry about duplicates.

## Lint compliance

The generated pages pass `keeba lint` out of the box — frontmatter, title, summary, sources, see-also all in canonical form.

## What you can do with this

- `keeba search "BM25 v0.1"` → returns PR #2's decision page with the rationale.
- Plug into Claude Code via `keeba mcp install --tool claude-code`. Now Claude can answer "why" questions about your code that previously needed a human in the loop.
- Schedule the ingest via GitHub Actions or claude.ai routine; the wiki tracks your PR rationale automatically.

## Sources

- `keeba ingest github --execute` (this command) — see [`internal/ingest/github.go`](https://github.com/aomerk/keeba/blob/main/internal/ingest/github.go).
- The PR pages in [`decisions/`](decisions/) were generated on 2026-04-28 from `aomerk/keeba` PR history.
