# keeba examples

Worked recipes that show keeba bootstrapping a wiki for a real codebase.

## Available examples

- **[`llm-c/`](llm-c/README.md)** — wiki for Karpathy's `llm.c`. Educational
  C codebase, small enough to dogfood `keeba bench` against.

## What an example contains

- `README.md` with the exact `keeba init` / `keeba meta` / `keeba bench`
  invocations.
- (eventually) checked-in `_bench/<date>.md` so the headline ratio in the
  top-level README has a real source.

## Adding your own

```bash
keeba init my-corpus-wiki
cd my-corpus-wiki
# edit keeba.config.yaml to point drift.repo_prefixes at your code
keeba meta
keeba bench --raw ../path/to/your/code
```

If the bench number is interesting, open a PR adding `examples/<your-corpus>/`
with the recipe and the bench output.
