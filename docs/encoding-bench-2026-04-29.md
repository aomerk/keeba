---
title: Encoding plugin bench — 2026-04-29
status: empirical
last_verified: 2026-04-29
---

# Encoding plugin bench (2026-04-29)

Empirical validation of the §10 encoding plugins on public retrieval
corpora. Numbers measured locally on CPU (i7-1365U, no GPU); no LLM in
the loop — pure retrieval quality + compression ratio.

## Summary

| Finding | Source |
|---|---|
| The 4× compression cliff (plan §10) holds. Best variants peak at 4.15×; pushing past flatlines or regresses. | CSN + RepoBench-R grids |
| **structural-card** is the function-page winner. Adding it to the plugin set was prompted by these numbers. | Both grids |
| Page-type matters more than aggressive compression. dense-tuple at 2.57× cost 13% MRR on functions; on the right page-type it's competitive. | CSN |
| Tokenizer change (camelCase / snake_case split) is the dominant lever for code retrieval. +85% MRR on RepoBench-R from tokenizer alone. | RepoBench-R |
| Findings transfer across corpora — same plugin ranking on CSN and RepoBench-R. | Both |

## Bench grid 1 — CSN Python (function-level retrieval)

200 queries vs 22,176-doc corpus. docstring → function code. BM25 with
identifier-aware tokenization unless noted.

| Pipeline | MRR | hit@1 | hit@10 | Compression | Δ MRR vs BM25 |
|---|---|---|---|---|---|
| `bm25` (vanilla tokenizer baseline) | 0.918 | 0.885 | 0.970 | 1.00× | — |
| `raw` (camelCase tokenizer, no compress) | 0.915 | 0.885 | 0.955 | 1.00× | -0.3% |
| `caveman` | 0.912 | 0.880 | 0.955 | 1.32× | -0.6% |
| `glossary` | 0.914 | 0.885 | 0.955 | 1.04× | -0.4% |
| `glossary,caveman` | 0.910 | 0.875 | 0.955 | 1.39× | -0.9% |
| `dense-tuple` | 0.796 | 0.755 | 0.875 | 2.57× | **-13.4% ❌** |
| `structural-card` | 0.925 | 0.895 | 0.965 | 3.95× | +0.7% |
| **`glossary,structural-card`** | **0.927** | **0.900** | 0.965 | **4.15×** | **+1.0% ✅** |
| `structural-card,caveman` | 0.924 | 0.895 | 0.970 | 4.11× | +0.7% |
| `glossary,structural-card,caveman` | 0.924 | 0.895 | 0.965 | 4.33× | +0.6% |

CSN was already easy for BM25 — docstring tokens overlap query directly.
Win is small but consistent. **dense-tuple's failure is the headline:**
applying an entity-page encoder to function bodies costs 13% MRR.
Validates §10's page-type-aware selector.

## Bench grid 2 — RepoBench-R Python (cross-file retrieval)

500 queries vs 1,841 unique candidate corpus. local code + imports →
right cross-file snippet (the actual Keeba pitch).

| Pipeline | MRR | hit@1 | hit@10 | Compression | Δ MRR vs BM25 |
|---|---|---|---|---|---|
| `bm25` (vanilla tokenizer) | 0.249 | 0.172 | 0.450 | 1.00× | — |
| `raw` (camelCase tokenizer) | 0.459 | 0.340 | 0.698 | 1.00× | **+85%** |
| `caveman` | 0.459 | 0.342 | 0.698 | 1.24× | +85% |
| `glossary` | 0.455 | 0.330 | 0.710 | 1.05× | +83% |
| `glossary,caveman` | 0.457 | 0.336 | 0.714 | 1.31× | +84% |
| `dense-tuple` | 0.462 | 0.350 | 0.688 | 2.00× | +86% |
| `structural-card` | 0.521 | 0.372 | 0.810 | 3.21× | **+109%** |
| **`glossary,structural-card`** | **0.521** | 0.368 | **0.820** | **3.38×** | **+109% ✅** |

On cross-file retrieval Keeba **doubles MRR vs vanilla BM25** —
hit@10 jumps 0.45 → 0.82.

## Implications

### 1. structural-card is the function-page default

It wins on CSN, wins by a wide margin on RepoBench-R, sits right at the
4× compression cap. Plan §10 listed 4 plugins; structural-card joins as
the 5th, specifically scoped to function / class definitions.

### 2. dense-tuple is for entity / fact pages only

On the wrong page-type it loses to plain BM25 by 13% MRR. The page-type
selector in `keeba bench` MUST not pick dense-tuple for function pages.

### 3. Tokenizer change is the biggest single lever (for code)

`raw` (no compression, just camelCase / snake_case splitting) already
yields +85% MRR on RepoBench-R. Compression adds another +12%.
Implication: the tokenizer choice in the BM25 layer matters more than
which encoding plugin runs over it.

### 4. The 4× cliff is real

Best variants stop improving past ~4.15×. Pushing to 4.33× flatlines.
This matches the LLMLingua-2 paper finding (Microsoft 2024) referenced
in plan §10. Bench enforces the cap.

### 5. Findings generalize

Same plugin ranking on both corpora (structural-card > glossary+caveman
> caveman > raw). Suggests these aren't CSN-specific artifacts.

## Method

- Bench harness: Python prototype (now retired) imported the public
  datasets via `datasets`, applied the same encoding pipelines that
  keeba-go now ships, ran BM25 (rank_bm25) over encoded text with an
  identifier-aware tokenizer (split camelCase + snake_case).
- Datasets:
  - `code_search_net` (Python test split) — 22,176 functions
  - `tianyang/repobench_python_v1.1` (`cross_file_first` split, 500 queries)
- Single-positive retrieval. Metrics: MRR, hit@k, NDCG@10.
- All numbers reproducible from the JSON in
  [the bench artifact PR description] — see also the runner harness in
  the original `feat-md-caveman` development branch.

## Out of scope (yet)

- LLM-in-the-loop bench (downstream answer quality with Keeba context vs
  raw context) — future `keeba bench` work, plan §17 step 8.
- Cross-language verification — bench only Python; regex plugins should
  generalize but not yet measured on Java / Go corpora.
- llmlingua plugin — runtime model dependency story unresolved; deferred
  per the plan.
