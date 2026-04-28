#!/usr/bin/env bash
# dogfood.sh — exercise the keeba binary end-to-end.
#
# Builds keeba from source, scaffolds a fresh wiki via `keeba init`, then
# exercises lint / drift / meta / search / bench / ingest / mcp serve, and
# asserts every step exits cleanly. Run this before opening a PR.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")" && pwd)
SCRATCH=$(mktemp -d)
WIKI="$SCRATCH/demo-wiki"
trap 'rm -rf "$SCRATCH"' EXIT

echo "==> build keeba"
( cd "$ROOT" && go build -o "$SCRATCH/keeba" ./cmd/keeba )
BIN="$SCRATCH/keeba"

echo "==> keeba --version"
"$BIN" --version

echo "==> keeba init demo-wiki"
( cd "$SCRATCH" && "$BIN" init demo-wiki --purpose "dogfood smoke" )
test -f "$WIKI/SCHEMA.md" || { echo "FAIL: SCHEMA.md not scaffolded"; exit 1; }

echo "==> keeba lint (fresh wiki, expect clean)"
"$BIN" lint --wiki-root "$WIKI"

echo "==> keeba drift (no prefixes configured, expect clean)"
"$BIN" drift --wiki-root "$WIKI"

echo "==> keeba meta"
"$BIN" meta --wiki-root "$WIKI"
test -f "$WIKI/_meta.json" || { echo "FAIL: _meta.json missing"; exit 1; }

echo "==> keeba meta --check (should be up to date)"
"$BIN" meta --check --wiki-root "$WIKI"

echo "==> keeba search 'getting started' (expect ≥1 hit)"
"$BIN" search "getting started" --wiki-root "$WIKI"

echo "==> keeba bench (writes _bench/<date>.md)"
"$BIN" bench --wiki-root "$WIKI" --raw "$WIKI" --top-k 3
test -d "$WIKI/_bench" || { echo "FAIL: _bench/ missing"; exit 1; }

echo "==> keeba ingest git --dry-run (prints template)"
"$BIN" ingest git --dry-run | head -3

echo "==> keeba mcp serve (initialize over stdio)"
echo '{"jsonrpc":"2.0","id":1,"method":"initialize"}' \
  | "$BIN" mcp serve --wiki-root "$WIKI" \
  | head -1 | grep -q '"protocolVersion"' \
  || { echo "FAIL: mcp serve did not respond with protocolVersion"; exit 1; }

echo "==> dogfood complete"
