---
tags: [agent, ingest]
last_verified: 2026-04-28
status: current
---

# git-ingest agent

> Weekly. Walks recent git activity and digests it into wiki updates.

## Mission

Read the git history of the source repos that this wiki covers (configured
in `keeba.config.yaml` under `drift.repo_prefixes`). Identify durable signal
— architectural shifts, schema changes, post-mortem-worthy incidents — and
write it up as wiki pages or append to `log.md`.

## Schedule

```
0 6 * * MON   # 06:00 UTC every Monday
```

Suggested host: GitHub Actions in this repo, or a claude.ai routine if the
team has Anthropic subscription quota to spend.

## What counts as "durable signal"

| Pattern | Action |
|---|---|
| Merge commit on a PR labeled `architecture` or `decision` | Open a `decisions/<slug>.md` if missing; otherwise append to one. |
| `BREAKING:` / `BREAKING CHANGE:` in a commit message | Append a `## [DATE] breaking: …` entry to `log.md` and link from the affected entity page. |
| Post-mortem keyword (`incident`, `outage`, `RCA`) in commit body | Open `investigations/<date>-<slug>.md`. |
| Dependency bump that crosses major versions | Append to the affected entity page's `## Dependencies` section. |
| Plain "fix typo" / formatting / lint commits | Skip. |

## What NEVER goes in the wiki

- Customer DMs, partner channels, anything tagged `private:`.
- Secrets, tokens, anything matching common credential patterns.
- One-line fix commits that aren't tied to a durable concept.

## Output rules

- Every new page must satisfy `keeba lint --file <new-path>` before committing.
- Append new entries to `log.md` rather than creating dated daily files.
- Cross-reference `[[wiki links]]` so the See Also sections of related pages
  point at each other.

## How the agent runs

1. Read `keeba.config.yaml` to find which repos to walk.
2. `git -C <repo> log --since=8.days --no-merges` for each.
3. Score each commit by the patterns above. Skip silently if score is low.
4. For each kept commit, draft the wiki page or `log.md` entry.
5. Run `keeba lint --file <draft>` and `keeba drift --file <draft>`. Fix
   anything red before committing.
6. Open one PR titled `git-ingest YYYY-MM-DD: N commits digested`. Body lists
   the source commits with hashes.

## Sources

- `keeba init` scaffolded this prompt template.
- See [keeba/README.md](https://github.com/aomerk/keeba) for the broader
  ingest-agent concept.

## See Also

- [[slack-ingest]]
- [[index]]
