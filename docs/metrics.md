# Metrics Report

Generated: 2026-10-04 07:26:11 UTC  |  Go: go1.27.0  |  OS/arch: windows/amd64  |  Commit: ceab0b6

Reproduce: `go run . bench`

## M1 — Token cost: JSON vs TOON (AI-bound payloads)

| Fixture | JSON bytes | JSON tokens | TOON bytes | TOON tokens | Savings |
|---|---:|---:|---:|---:|---:|
| conflict.py | 1251 | 313 | 1202 | 301 | 3.8% |
| conflict.js | 1331 | 333 | 1282 | 321 | 3.6% |
| conflict.ts | 1504 | 376 | 1461 | 365 | 2.9% |
| conflict.go | 1383 | 346 | 1334 | 334 | 3.5% |
| Conflict.java | 2444 | 611 | 2176 | 544 | 11.0% |
| Conflict.cs | 2434 | 609 | 2166 | 542 | 11.0% |
| conflict.c | 1259 | 315 | 1240 | 310 | 1.6% |
| conflict.cpp | 1277 | 320 | 1258 | 315 | 1.6% |
| conflict.rs | 1362 | 341 | 1319 | 330 | 3.2% |
| conflict.rb | 2017 | 505 | 1749 | 437 | 13.5% |
| conflict.php | 1379 | 345 | 1330 | 333 | 3.5% |
| conflicts/sample2.py | 169126 | 42282 | 163629 | 40907 | 3.3% |
| conflicts/sampleConflict.js | 6383 | 1596 | 5596 | 1399 | 12.3% |

**Aggregate**: 48292 JSON tokens -> 46438 TOON tokens (3.8% smaller) across 13 payload(s).

## M2 — AST cache latency (cold vs warm)

| Metric | Cold (µs) | Warm (µs) |
|---|---:|---:|
| mean | 492.0 | 10.5 |
| p50 | 482.5 | 10.5 |
| p95 | 521.1 | 10.5 |

**Speedup**: 46.8x over 5 sample(s).

## M3 — End-to-end analyze (fresh vs warm AST cache)

| Fresh cache (ms) | Warm cache (ms) | Faster |
|---|---:|---:|
| 142.3 | 140.3 | 1.4% |

## M4 — README pre-context cost

| Fixture | Without (tokens) | With (tokens) | Added | Share of context |
|---|---:|---:|---:|---:|
| conflict.ts | 83 | 1109 | 1026 | 92.5% |

## M5 — Soft prompt budget trimming (AI_TARGET_PROMPT_TOKENS)

| Fixture | Before (tokens) | After (tokens) | Trimmed | Functions | Variables |
|---|---:|---:|---:|---|---|
| conflict.ts | 1500 | 83 | 94.5% | 1 -> 1 | 0 -> 0 |

## M6 — Suggestion reuse across consecutive generations

| Collisions | Pass 1 reused | Pass 1 generated | Pass 2 reused | Pass 2 generated |
|---|---:|---:|---:|---:|
| 2 | 2 | 0 | 2 | 0 |
