---
tags: [agent, ingest, slack]
last_verified: 2026-04-28
status: current
---

# slack-ingest agent

> Daily. Digests Slack threads into investigation pages.

See [`agents/slack-ingest.md`](slack-ingest.md) in your wiki for the live
copy of this prompt — `keeba init` and `keeba ingest slack` write it there.

## Mission

Read recent Slack threads from the configured channels. Per thread (NOT
per message), decide whether the thread carries durable signal worth a
wiki page. If yes, write a schema-conformant `investigations/` page;
otherwise, skip silently.

## Schedule

`0 6 * * *` — daily 06:00 UTC. Suggested host: a claude.ai routine, since
Slack MCP is already there and the schedule is funded by the user's Claude
Code subscription rather than per-token API spend.

## Heuristics (a thread qualifies if **all** are true)

1. ≥ 5 replies (filters drive-by chats).
2. Contains a decision verb: `decided`, `agreed`, `concluded`, `picked`,
   `dropped`, `pivoted`, `chose`, `confirmed`, `rejected`.
3. Mentions a named entity from this wiki (customer, vendor, product,
   concept).
4. Has a resolution — the last 3 messages aren't all questions.

## Anti-patterns enforced

- No raw transcripts — the agent digests, doesn't quote verbatim.
- No customer DMs — only public channels listed in this prompt.
- No PII — strip emails, phones, last names; use roles.

## Sources

- `keeba ingest slack` writes this template into your wiki's `agents/` dir.

## See Also

- [[git-ingest]]
