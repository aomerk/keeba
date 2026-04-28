---
tags: [agent, ingest, github]
last_verified: 2026-04-28
status: current
---

# github-ingest agent

> Daily / weekly. Walks merged PRs and digests the durable signal — decisions, post-mortems, breaking changes — into the wiki.

## Why this is the moat

Your code is in your repo. Your AI tools can already read it. But the *why* — why we picked OpenSearch over Elastic, why the rule engine got rewritten, why the JWT validation broke last quarter — lives in PR descriptions and comment threads. That signal **isn't in your code** and dies in GitHub the moment the PR merges.

`keeba ingest github --execute` extracts it.

## Mission

Pull merged PRs from configured repos. Classify by regex on title + label-aware rules. Write durable signal to:

- `decisions/<slug>.md` — for architecture / ADR / decision-labeled PRs
- `investigations/<date>-pr-<n>-<slug>.md` — for incident / post-mortem / hotfix PRs
- `log.md` appends — for breaking changes and major-version dep bumps

Idempotent across runs (each page records `pr_number: <n>` in frontmatter; re-runs skip already-imported PRs).

## Usage

```bash
# from inside your wiki:
keeba ingest github --execute --since 30d
# or explicitly:
keeba ingest github --execute --github-repo aomerk/keeba --since 90d --github-limit 500
keeba ingest github --execute --since 30d --dry-run    # preview
```

## Schedule

Daily 06:00 UTC works for most teams:

```
0 6 * * *
```

Run from a GitHub Action with the same `gh` CLI auth, or locally on a cron with your `gh auth login` token.

## Anti-patterns enforced

- **No PR bodies that don't carry signal.** Plain `feat:` / `fix:` / `chore:` commits without architectural context are skipped silently.
- **No verbatim transcripts.** The PR body is wrapped with frontmatter and `## Sources` / `## See Also`, but it's a draft — humans should curate.
- **Idempotency.** Re-running the agent doesn't duplicate pages.

## Sources

- [`internal/ingest/github.go`](https://github.com/aomerk/keeba) — implementation.

## See Also

- [[git-ingest]]
- [[slack-ingest]]
