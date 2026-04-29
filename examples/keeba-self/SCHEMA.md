# SCHEMA

> Conventions every page in this wiki must follow. Enforced by `keeba lint`.

## Frontmatter (required)

```yaml
---
tags: [topic, area]
last_verified: 2026-04-28
status: current   # current | draft | archived | deprecated | proposed
cited_files: []   # optional; populated automatically by `keeba meta`
---
```

## Page structure

Every page must contain, in this order:

1. `# Title` — single-line level-1 heading.
2. `> One-line summary.` — within the first 5 lines after the title.
3. Body content with whatever level-2+ headings make sense.
4. `## Sources` — bullet list of citations (URLs, repo paths, conversations).
5. `## See Also` — bullet list of `[[wiki link]]` pointers to related pages.

Any deviation fails `keeba lint`.

## File names

- Lowercase, hyphenated: `my-page.md`, `customer-x-incident.md`.
- Allowed uppercase: `SCHEMA.md`, `README.md`, `QUERY_PATTERNS.md`.
- Dated logs (`2026-04-28.md`) bypass the casing rule.

## Wiki links

`[[other-page]]`, `[[other-page|Alias]]`, and `[[other-page#anchor]]` all
resolve via `keeba lint` against the actual file tree.

## Categories

Top-level directories define category. v0.1 default tree:

| Dir | What lives there |
|---|---|
| `entities/` | Things (services, products, people, customers, vendors). |
| `concepts/` | Ideas (architectures, conventions, definitions). |
| `investigations/` | Bugs, incidents, customer issues, post-mortems. |
| `decisions/` | ADRs and durable choices the team has made. |

## Sources

- `keeba lint` enforces every rule above.

## See Also

- [[index]]
- [[log]]
