# Example: keeba on `karpathy/llm.c`

A worked example showing keeba bootstrapping a wiki for a small, popular,
real-world Go-adjacent codebase. The repo isn't checked in here — clone it
yourself and follow along.

## Recipe

```bash
# 1. clone the upstream code
git clone https://github.com/karpathy/llm.c ~/llm-c
cd ~

# 2. scaffold a wiki next to it
keeba init llm-c-wiki
cd llm-c-wiki

# 3. tell drift where the source lives
$EDITOR keeba.config.yaml   # set drift.repo_prefixes = ["llm.c/"]
                            # set drift.gigarepo_root = "../"

# 4. seed the index
keeba meta

# 5. ask it questions
keeba search "fp16 forward pass"
keeba search "weight initialization"
keeba search "layer norm gradient"

# 6. measure ROI vs. raw reads
keeba bench --raw ../llm.c --top-k 5
```

## Why llm.c

- Small (~5k LOC C + Python).
- Self-contained (no monorepo, no build system to fight).
- Educational: every file is dense with concepts a wiki *should* index.
- Well-known: bench numbers from this repo are reproducible by anyone.

## What you should see

The first `keeba search` returns top-5 wiki chunks — empty until you add
some pages by hand or via `keeba ingest git`.

The first `keeba bench`, run against an empty wiki, shows the baseline:
wiki tokens ≈ a few hundred, raw tokens ≈ several hundred thousand. The
ratio gets meaningful once the wiki actually has pages — which is the
point. Track the ratio over time: as the wiki matures, the ratio improves.

## Next steps after the bench

1. Run the [git-ingest agent template](../../internal/scaffold/templates/agents/git-ingest.md)
   in your AI tool (Claude Code, Cursor, Codex, claude.ai routine).
2. The agent walks `git log` of `~/llm-c` and writes durable signal into
   `concepts/`, `decisions/`, `investigations/`.
3. Re-run `keeba bench` and watch the wiki tokens drop relative to raw.

## Sources

- [Karpathy's llm.c](https://github.com/karpathy/llm.c)
- This file is the spec; actual numbers will be appended once a maintainer
  runs the example end-to-end.

## See Also

- [`../README.md`](../README.md)
- [keeba README](../../README.md)
