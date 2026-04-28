#!/usr/bin/env bash
# dogfood.sh — exercise the keeba binary end-to-end against a tiny sample wiki.
# Not run in CI; run locally before opening a PR to verify the CLI does what
# the README claims it does.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")" && pwd)
BIN=$(mktemp -d)/keeba
SAMPLE=$(mktemp -d)
trap 'rm -rf "$BIN" "$SAMPLE"' EXIT

echo "==> build keeba"
( cd "$ROOT" && go build -o "$BIN" ./cmd/keeba )

echo "==> scaffold sample wiki at $SAMPLE"
mkdir -p "$SAMPLE/concepts"
cat >"$SAMPLE/concepts/alpha.md" <<'MD'
---
tags: [test]
last_verified: 2026-04-28
status: current
---

# Alpha

> First page.

## Sources

## See Also
- [[beta]]
MD

cat >"$SAMPLE/concepts/beta.md" <<'MD'
---
tags: [test]
last_verified: 2026-04-28
status: current
---

# Beta

> Second page.

## Sources

## See Also
- [[alpha]]
MD

cat >"$SAMPLE/concepts/broken.md" <<'MD'
---
tags: [test]
last_verified: 2026-04-28
status: current
---

# Broken

> Page that links to nothing.

## Sources

## See Also
- [[nonexistent]]
MD

echo "==> keeba lint --file alpha.md (should pass)"
"$BIN" lint --wiki-root "$SAMPLE" --file "$SAMPLE/concepts/alpha.md"

echo "==> keeba lint (should fail with broken-wikilink)"
if "$BIN" lint --wiki-root "$SAMPLE"; then
    echo "FAIL: expected lint to exit non-zero on broken wikilink"; exit 1
fi

echo "==> keeba meta (should write _meta.json)"
"$BIN" meta --wiki-root "$SAMPLE"
test -f "$SAMPLE/_meta.json" || { echo "FAIL: _meta.json missing"; exit 1; }

echo "==> keeba meta --check (should be up to date)"
"$BIN" meta --check --wiki-root "$SAMPLE"

echo "==> keeba init test (stub, expect exit 2)"
set +e
"$BIN" init test
code=$?
set -e
test "$code" = 2 || { echo "FAIL: stub exit code $code, want 2"; exit 1; }

echo "==> dogfood complete"
