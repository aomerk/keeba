# keeba MCP bench — go-ethereum

_2026-05-01 07:13:15 UTC_

## Index

| Metric | Value |
|---|---|
| Repo | `/tmp/go-ethereum` |
| Files indexed | 1405 |
| Symbols | 20067 |
| Call edges | 138442 |
| Compile time | 8590 ms |
| Index size on disk | 35.8 MiB |

## Receipt

- **654.2× cheaper** in returned bytes vs unfiltered alternative
- bytes_returned: 46.0 KiB | bytes_alternative: 29.4 MiB | tokens_saved: 7689551

## Per-query

| Query | Tool | Latency | Returned | Alternative | Hits |
|---|---|---|---|---|---|
| find_def main | find_def | 0 ms | 2.2 KiB | 91.7 KiB | 10 |
| find_def Run | find_def | 0 ms | 2.9 KiB | 58.3 KiB | 10 |
| search_symbols 'http handler' | search_symbols | 21 ms | 4.0 KiB | 29.9 KiB | 10 |
| search_symbols 'config load' | search_symbols | 26 ms | 4.3 KiB | 83.9 KiB | 10 |
| grep_symbols 'os.Getenv' (literal) | grep_symbols | 229 ms | 2.9 KiB | 14.3 MiB | 13 |
| grep_symbols 'context.Context' (literal) | grep_symbols | 28 ms | 6.5 KiB | 14.3 MiB | 25 |
| find_callers main | find_callers | 0 ms | 200 B | 10.3 KiB | 1 |
| find_refs Block | find_refs | 0 ms | 4.2 KiB | 99.8 KiB | 25 |
| tests_for Run | tests_for | 32 ms | 6.3 KiB | 293.9 KiB | 25 |
| summary cmd/ | summary | 8 ms | 12.7 KiB | 70.9 KiB | 50 |

## Notes

- Receipt totals come from the in-process `session_stats` snapshot — the same number an agent reads at runtime.
- `Alternative` per row is the byte-size sum the agent would have pulled in unfiltered to reach the same result without keeba. Each tool replays its own filter logic against the symbol graph, sums distinct file sizes via `os.Stat`, and contributes only the result set the user actually saw (bounded by limits + filters).
