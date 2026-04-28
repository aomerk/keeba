---
tags: [agent, ingest]
last_verified: 2026-04-28
status: current
---

# slack-ingest agent

> Daily. Digests Slack threads into investigation pages.

## Mission

Read recent Slack activity from the channels listed in this prompt. Per
thread (NOT per message), decide whether the thread carries durable signal
worth a wiki page. If yes, write a schema-conformant `investigations/`
page; otherwise, skip silently.

## Schedule

```
0 6 * * *   # 06:00 UTC daily
```

Suggested host: a claude.ai routine — Slack MCP is already there and the
schedule is funded by your Claude Code subscription rather than per-token
API spend.

## "Durable signal" heuristic

A thread qualifies if **all** of these are true:

1. ≥ 5 replies (filters drive-by chats).
2. Contains at least one decision verb: `decided`, `agreed`, `concluded`,
   `picked`, `dropped`, `pivoted`, `chose`, `confirmed`, `rejected`.
3. Mentions a named entity from this wiki: customer, vendor, product, or a
   concept already in `concepts/`.
4. Has a resolution — the last 3 messages aren't all questions.

## Channels

Edit this list. The agent reads only what's enumerated here.

```yaml
channels:
  # - "#engineering"
  # - "#customer-foo"
  # - "#incidents"
```

## Output target

For each qualifying thread, write `investigations/<YYYY-MM-DD>-<slug>.md`:

```markdown
---
tags: [slack-ingest, <channel>]
last_verified: <today>
status: current
---

# <one-line title summarizing the thread>

> <2-sentence summary of the conclusion or decision>.

## What happened
<chronological digest, anonymized: "the engineer raised X" not "@user1234 said X">

## Decision / Outcome
<what the team agreed or decided>

## Sources
- Slack thread: <permalink>

## See Also
- [[<related entity>]]
```

## Anti-patterns enforced

- **No raw transcripts.** The agent digests; it doesn't quote messages
  verbatim. Verbatim leaks tone, identifies people, ages badly.
- **No customer DMs.** Only public channels listed above.
- **No PII.** Strip emails, phone numbers, last names. Use roles ("the
  customer's CTO", "an engineer on the platform team").

## How the agent runs

1. List threads in each channel from the last 25h.
2. Filter by the heuristic above.
3. For each survivor, draft the page.
4. Run `keeba lint --file <draft>`. Fix anything red.
5. Open one PR titled `slack-ingest YYYY-MM-DD: N threads digested`.

## Sources

- `keeba init` scaffolded this prompt template.

## See Also

- [[git-ingest]]
- [[index]]
