# Example: keeba on `karpathy/llm.c`

A worked example showing keeba bootstrapping a wiki for a small, well-known
real-world codebase: Karpathy's `llm.c`, a reference GPT-2 implementation
in plain C/CUDA (~5k LOC, ~1.9 MB). The bench output below is real — see
[`_bench/`](_bench/) for the verbatim run.

## Headline number

```
keeba: 2465.9× cheaper, 80.1× faster (5 questions; byte-count mode)
```

That's wiki-mode (BM25 top-5 chunks) vs raw-mode (every text file in the
repo concatenated) for the bundled 5-question code-project bench.

The wiki was a freshly-scaffolded one — the only page in it is the
`getting-started.md` seed. As real pages get added (entity / concept /
investigation pages from `keeba ingest git`), wiki tokens go *up* and the
ratio compresses, but raw tokens stay flat — so the ratio remains a
useful staleness signal over time.

## Reproducing the run

```bash
# 1. clone the upstream code
git clone --depth=1 https://github.com/karpathy/llm.c ~/llm.c

# 2. scaffold a wiki next to it
cd ~/your-workspace
keeba init llm-c-wiki
cd llm-c-wiki

# 3. seed the index
keeba meta

# 4. run the bench
keeba bench --raw ~/llm.c --top-k 5 --max-raw-chars 200000
```

You should see the same headline within a small constant factor (the wiki
tokens come from the snippet sizes; the raw tokens come from the
`max-raw-chars`-truncated dump).

## Going further: real LLM bench

The byte-count mode above is an input-volume sanity signal. To measure
what an LLM actually consumes when answering, set `ANTHROPIC_API_KEY` and
run with `--llm anthropic`:

```bash
export ANTHROPIC_API_KEY=sk-...
keeba bench --raw ~/llm.c --top-k 5 --llm anthropic
```

The output captures input + output tokens *from the API response* (not
estimates), wall time per call, and a self-rated 1–5 confidence per
question. The model used is configurable via `KEEBA_LLM_MODEL`; default
is `claude-haiku-4-5`. Cost: ~$0.10–$0.20 per 5-question run on Haiku.

## Why llm.c

- **Small** (~5k LOC C + Python + CUDA) — bench runs in seconds.
- **Self-contained** (no monorepo, no build system to fight).
- **Educational** — every file is dense with concepts a wiki *should* index.
- **Well-known** — bench numbers from this repo are reproducible by anyone.

## What you should see grow over time

The first run shows a huge ratio because the wiki is empty. As you fill
it with real pages (via `keeba ingest git` or by hand), the wiki gets
larger but raw stays the same — the ratio shrinks toward the "real" value
that reflects the curated/raw signal-to-noise of your corpus.

A healthy wiki for a codebase like llm.c should land around **20×–80×
cheaper** with comparable or higher confidence than raw-mode, on a 5-question
default bench. Anything above 200× usually means the wiki is too sparse to
be useful for actual questions — wiki-mode is winning by *not answering*.

## Sources

- [Karpathy's llm.c](https://github.com/karpathy/llm.c) — the corpus.
- [`_bench/2026-04-28-0907.md`](_bench/2026-04-28-0907.md) — the verbatim run that backs the headline number.

## See Also

- [`../README.md`](../README.md) — examples index.
- [keeba README](../../README.md)
