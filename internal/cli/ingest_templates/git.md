---
tags: [agent, ingest, git]
last_verified: 2026-04-28
status: current
---

# git-ingest agent

> Weekly. Walks recent git activity and digests it into wiki updates.

See [`agents/git-ingest.md`](git-ingest.md) in your wiki for the live copy
of this prompt — `keeba init` and `keeba ingest git` write it there.

## Mission

Read the git history of the source repos this wiki covers (configured in
`keeba.config.yaml` under `drift.repo_prefixes`). Identify durable signal —
architectural shifts, schema changes, post-mortem-worthy incidents — and
write it up as wiki pages or append to `log.md`.

## Schedule

`0 6 * * MON` — 06:00 UTC every Monday.

## Heuristics

| Pattern | Action |
|---|---|
| Merge commit on a PR labeled `architecture` or `decision` | Open `decisions/<slug>.md` if missing; otherwise append. |
| `BREAKING:` / `BREAKING CHANGE:` in a commit | Append `## [DATE] breaking: …` to `log.md`; link from the affected entity page. |
| `incident` / `outage` / `RCA` keywords | Open `investigations/<date>-<slug>.md`. |
| Major-version dep bump | Append to the affected entity's `## Dependencies` section. |
| Plain typo / formatting / lint | Skip. |

## Sources

- `keeba ingest git` writes this template into your wiki's `agents/` dir.

## See Also

- [[slack-ingest]]
