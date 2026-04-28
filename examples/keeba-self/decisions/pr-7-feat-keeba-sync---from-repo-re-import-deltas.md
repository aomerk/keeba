---
tags: [decision, github-ingest]
last_verified: 2026-04-28
status: current
pr_number: 7
---

# feat: keeba sync --from-repo (re-import deltas, preserve hand-edits)

> Auto-imported from PR #7. Edit to match the real ADR shape.

## Context

Solves the killer failure mode of \`keeba init --from-repo\`: the wiki rots the moment the source repo's docs change. Without sync, the importer is a one-shot toy.

## What's new

\`\`\`bash
keeba sync --from-repo ../my-codebase
# sync from ../my-codebase: 3 updated, 1 preserved (edited)
#   ↻ concepts/readme.md
#   ↻ concepts/architecture.md
#   ↻ concepts/docs-deployment.md
#   ✋ concepts/contributing.md (skipped — locally edited)
\`\`\`

## How it tells pristine from edited

Imported pages now carry \`keeba_pristine_hash: <sha256>\` in frontmatter — the hash of the wrapper's canonical body output.

On sync, the page is **pristine** if the hash matches the body on disk → safe to overwrite. **Edited** otherwise → preserved.

Removing the hash (or any body change) takes the page off the sync path. That's the explicit escape hatch: \"I don't want keeba touching this page anymore.\"

Manual pages (created without \`--from-repo\`) have no hash → always preserved.

## End-to-end verified on llm.c

\`\`\`
== Step 1: init --from-repo ==
  + concepts/readme.md
  + concepts/doc-layernorm-layernorm.md
  + concepts/scripts-readme.md

== Step 2: sync (source unchanged) ==
sync from /tmp/llm-c-bench/llm.c: 3 updated, 0 preserved (edited)

== Step 3: edit a page ==
  page now contains: 1 HAND-EDITED markers

== Step 4: sync skips edited page ==
sync from /tmp/llm-c-bench/llm.c: 2 updated, 1 preserved (edited)
  ↻ concepts/doc-layernorm-layernorm.md
  ↻ concepts/scripts-readme.md
  ✋ concepts/readme.md (skipped — locally edited)
  edits preserved: 1 HAND-EDITED markers

== Step 5: lint clean after sync ==
lint: clean
\`\`\`

## Tests pinned

- \`TestSyncRefreshesPristinePages\` — source v1 → v2 propagates
- \`TestSyncPreservesEditedPages\` — user-edited body is not clobbered
- \`TestSyncImportsNewSourceFiles\` — files added in source land in wiki
- \`TestSyncSkipsManualPages\` — pages without \`keeba_pristine_hash\` treated as manual

## Why this matters

\"Wiki rots after first import\" is the most common failure mode of every wiki product. Without sync, every keeba install dies in 90 days. With sync, a user can put \`keeba sync --from-repo .\` in a cron / GH Action and the wiki tracks upstream automatically while preserving everything they've curated by hand.

## Test plan

\`\`\`bash
git fetch && git checkout feat-sync-from-repo
go build -o /tmp/keeba ./cmd/keeba && go test ./... -race && golangci-lint run ./...

# repro the round-trip
git clone --depth=1 https://github.com/karpathy/llm.c /tmp/llm.c
TMP=\$(mktemp -d) && cd \$TMP
/tmp/keeba init demo --from-repo /tmp/llm.c
/tmp/keeba sync --from-repo /tmp/llm.c   # idempotent on unchanged source
sed -i 's/^# llm.c\$/# EDITED BY ME/' demo/concepts/readme.md
/tmp/keeba sync --from-repo /tmp/llm.c   # readme.md preserved; others refreshed
\`\`\`

🤖 Generated with [Claude Code](https://claude.com/claude-code)

## Sources

- pr: https://github.com/aomerk/keeba/pull/7
- merged: 2026-04-28T14:46:31Z
- author: @aomerk

## See Also

- [[log]]
