# 00 — High-Level Overview

## Purpose

**Commitment-Issues** is a Git merge-conflict analysis and resolution workbench.
Instead of treating a conflict as two flat text blocks, it parses all three
versions of every conflicted file (Base / Ours / Theirs) with Tree-sitter,
diffs them at the *structural symbol* level (functions, classes, methods,
variables, imports), and produces:

1. a semantic explanation of *what* collided (same symbol changed on both
   sides) vs. what changed cleanly on one side,
2. a scoped, token-budgeted prompt context (including a README excerpt for
   project grounding) for AI resolution,
3. per-collision AI suggestions with typed errors, retries, confidence
   scores, and full audit metadata,
4. a strictly approval-gated pipeline to preview, apply, validate, and roll
   back AI-suggested patches against the working tree,
5. a hypothetical-merge comparison of two commits or branches (analysis-only,
   no working-tree mutation).

The project is explicitly **not** an autonomous resolver: no code is ever
applied without explicit human approval, and every mutation is reversible.

## Architecture

The system is a layered pipeline with a hard separation between read-only
analysis and gated mutation:

```text
CLI (cobra)  ─┐
              ├─►  Engine (orchestration)  ─►  Git / Parser / Semantic / Context
HTTP server ──┘            │
                           ├─► AI providers (ollama / gemini / groq) — only on demand
                           ├─► Run-scoped state (runstate) — never package globals
                           ├─► AST cache (per-run / optional disk)
                           └─► Resolution stack (patch → approve → apply → validate → revert)
React SPA ◄── REST /api/* ─┘
```

Key design patterns and rules:

- **Run-scoped state.** All per-run data (analyses, prompt contexts, graphs,
  reports, suggestions, resolutions, history) lives in a single
  `runstate.Run` instance keyed by `(repository, file, collision)`. The only
  package-level state allowed is write-once init config (the AI provider
  registry). Cache instances are injected per run / per comparison, never
  global.
- **Three-way everything.** Every analysis is anchored on
  `base / ours / theirs` — from Git index stages (1/2/3), inline conflict
  markers (incl. diff3, malformed, nested, incomplete regions), or arbitrary
  commit refs (`merge-base` + `git show <rev>:<path>`).
- **Content-addressed incremental parsing.** AST extraction goes through a
  bounded LRU cache keyed by `(repo, path, content hash, language, parser
  version, schema version)`. Entries are immutable and cloned on every
  access; optional disk persistence uses checksummed JSON with corruption
  recovery.
- **Provider abstraction with registry.** AI providers implement one
  `Resolver` interface and self-register in `init()`. Config precedence is
  **CLI flags > env vars (`AI_*`) > provider defaults**; providers/models are
  never switched silently, and secrets are redacted from every error path.
- **Typed errors end-to-end.** Stable error codes (`PROVIDER_UNAVAILABLE`,
  `MODEL_MISSING`, `STALE_FILE`, `PATH_ESCAPE`, `NO_COMMON_ANCESTRY`-style
  sentinels like `git.ErrNoMergeBase` / `git.ErrFileNotInTree`, …) map
  deterministically to HTTP statuses and CLI output.
- **Approval-gated mutation.** The only write path is
  `preview → approve → apply → validate → (revert)`, guarded by
  analysis-time content hashes, region-context hashes, path-escape checks,
  atomic writes, and pre-apply snapshots. `git reset --hard` and
  AI-provided shell commands are forbidden by design.
- **Determinism.** Sorted file lists, stable identity keys
  (`file+scope+kind+name+signature`), fixed discovery orders — same input
  always yields the same output.
- **AI-free by default.** `scan`, `analyze`, `serve`, and `compare` never
  call an AI provider. Only `resolve` and `GET /api/suggestions` do.

## Tech Stack & Tooling

| Layer | Technology |
|---|---|
| Backend language | Go 1.26.3 (module `CommitIssues`) |
| CLI | `github.com/spf13/cobra` |
| Parsing | `github.com/smacker/go-tree-sitter` (cgo) — 13 grammars: Python, JS, TS/TSX, Go, Java, C, C++, C#, PHP, Ruby, Rust, HTML |
| Text handling | `golang.org/x/text` (BOM/UTF-8 normalization) |
| AI transport | stdlib `net/http` — OpenAI-compatible chat completions (Ollama, Groq) + Gemini `generateContent` |
| HTTP server | stdlib `net/http` mux, hand-rolled route registration |
| Frontend | React 19, Vite 8, react-router-dom, ESLint |
| Fixtures | `conflicts/` sample conflicted files (JS + Python) |
| Verification | `go test ./...`, `go test -race ./...`, `go vet ./...`, `npm run lint`, `npm run build` |

## System Map

```mermaid
graph TD
    subgraph Entry Points
        CLI[cmd: scan / analyze / resolve / serve / compare]
        SPA[React SPA: frontend/src]
    end

    subgraph Orchestration
        ENG[internal/engine: pipeline, config]
    end

    subgraph Read-only Analysis
        GIT[internal/git: conflict extraction, stages, refs, merge-base, renames]
        PARSER[internal/parser: Tree-sitter, tiers, per-language extraction]
        SEM[internal/semantic: SmartDiff, semantic graph, conflict scope]
        CTX[internal/context + internal/prompt: prompt context, README, payload]
        CMP[internal/compare: hypothetical 3-way merge analysis]
    end

    subgraph Performance
        CACHE[internal/cache: LRU + optional disk, immutable entries]
    end

    subgraph AI Layer
        AI[internal/ai: registry, config, http, health, validate]
        SUG[internal/suggestions: bounded concurrent generator]
    end

    subgraph State & Interfaces
        RS[internal/runstate: run-scoped state + history]
        API[internal/api: REST handlers]
        GRP[internal/graph + internal/report]
    end

    subgraph Mutation (approval-gated)
        RES[internal/resolutions]
        PATCH[internal/patch]
        APPLY[internal/apply]
        SAFE[internal/safeguard + internal/fileutil]
        VAL[internal/validation]
    end

    CLI --> ENG
    ENG --> GIT --> PARSER --> SEM --> CTX
    PARSER <--> CACHE
    CMP --> GIT
    CMP --> CACHE
    CMP --> SEM
    ENG --> RS
    ENG --> AI
    AI --> SUG --> RS
    RS --> API --> SPA
    SEM --> GRP --> RS
    SUG --> RES --> PATCH --> SAFE --> APPLY --> VAL
    APPLY --> RS
```

## Repository Layout

```text
Commitment-Issues/
├── backend/                  # Go application (module CommitIssues)
│   ├── main.go               # entry point → cmd.Execute()
│   ├── cmd/                  # cobra commands: root, scan, analyze, resolve, serve, compare
│   └── internal/
│       ├── engine/           # orchestration: scan root, conflict discovery, per-file pipeline
│       ├── git/              # conflict parsing, index stages, refs, merge-base, renames
│       ├── parser/           # Tree-sitter: language registry, extraction, diagnostics
│       ├── semantic/         # SmartDiff collisions, semantic graph, conflict scope
│       ├── context/          # prompt context IR, README pre-context, FileAnalysis
│       ├── prompt/           # AI request payload (TOON-ish text encoding)
│       ├── ai/               # provider registry/config/http/health/validate + 3 providers
│       ├── suggestions/      # bounded-concurrency suggestion generator
│       ├── runstate/         # run-scoped state, bounded history, resolution store
│       ├── resolutions/      # resolution lifecycle model
│       ├── patch/            # patch construction + unified-diff preview
│       ├── apply/            # atomic apply + snapshot rollback
│       ├── safeguard/        # staleness / path / repository safety checks
│       ├── validation/       # allow-listed post-apply commands (fmt/test/build)
│       ├── fileutil/         # hashing, atomic write, binary/size guards
│       ├── cache/            # AST cache: LRU memory + optional checksummed disk
│       ├── compare/          # two-commit/two-branch hypothetical merge analysis
│       ├── graph/            # Cytoscape-compatible graph payloads
│       ├── report/           # human-readable CLI report rendering
│       └── api/              # HTTP handlers (/api/*), static SPA serving
├── frontend/                 # React 19 + Vite 8 SPA
│   └── src/
│       ├── components/       # navbar, sidebar
│       └── tabs/             # dashboard, conflicts, treediff, suggestions,
│                             # commitgraph, history, aicontext
├── conflicts/                # sample conflicted fixtures for local runs
└── docs/                     # phase reports + this guide
```

## Current Feature Status (as of this documentation)

| Capability | Status |
|---|---|
| Conflict scan/analyze (staged + inline, diff3/malformed) | Complete |
| 13-language AST extraction with imports, scopes, signatures, calls, diagnostics | Complete |
| AI suggestions (3 providers, retries, health checks, redaction) | Complete |
| README pre-context (`--readme-context`, `AI_README_*`) | Complete |
| Resolution lifecycle (preview/approve/apply/validate/revert) + frontend | Complete |
| Commit/branch comparison CLI (rename-aware, typed errors) | Complete — CLI + `GET\|POST /api/compare`; frontend tab exists but is an empty 0-byte file (see Module_Frontend) |
| AST caching (per-run + comparison) | Complete — disk mode implemented, unwired |
| Tier-5 languages (Kotlin/Swift/Dart/SQL/shell/data formats) | Not started |
