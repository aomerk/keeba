---
tags: [meta]
last_verified: 2026-04-28
status: current
---

# keeba-self

> Wiki digesting keeba's own PR history

The agent's first read. Edit me.

## Categories

- [[SCHEMA]] — schema rules every page follows.
- `entities/` — services, products, customers, vendors.
- `concepts/` — architectures, conventions, definitions.
- `investigations/` — bugs, incidents, post-mortems.
- `decisions/` — durable team choices.
- [[log]] — chronological activity log.

## How to query this wiki

| Question shape | Where to look |
|---|---|
| "what is X?" | start here, follow the closest entity/concept |
| "when did Y change?" | [[log]] → `git log` |
| "what calls Z structurally?" | source repos (or future vector search) |
| "what did the customer say about W?" | `investigations/` |

## Sources

- `keeba init` scaffolded this on 2026-04-28.

## See Also

- [[SCHEMA]]
- [[log]]
