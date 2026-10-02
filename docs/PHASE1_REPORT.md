# Phase 1 — Stabilize Analysis: Deliverables

Scope: reliability, determinism, correctness, state isolation, error handling and
test coverage of the **Go analysis pipeline**. No AI workflow, apply/rollback
workflow or frontend feature work was performed (per the Phase 1 scope).

Verification (run from `backend/`):

```bash
go test ./...
go test -race ./...
go vet ./...
```

All three pass.

---

## 1. Implementation changes

### Configuration validation (`internal/engine/config.go` — new)
- Added `Config.Timeout` and `Config.DefaultConfig()`.
- `Config.Validate()` runs **before any scanning** and enforces, in a fixed,
  deterministic order:
  - `MaxConcurrency >= 1` (a non-positive value would deadlock the semaphore),
  - `Timeout > 0`,
  - `0 <= ConfidenceThreshold <= 100`.
- Introduced a stable `ErrorCode` enum and a `CodedError` interface.
- Invalid config returns a structured `*ConfigError` and produces a **non-zero
  CLI exit status**; analysis never starts.

### Context propagation & cancellation
- `context.Context` now threads through `ProcessRepository`, `ProcessConflictFile`,
  Git operations (`FindGitRepositoryRoots`, `GetConflictedFiles`,
  `ExtractConflictVersions`, `git show` via `exec.CommandContext`), Tree-sitter
  parsing (`ParseCtx`), semantic analysis (`semantic.GenerateSmartDiffContext`)
  and AI resolution.
- Workers acquire the concurrency semaphore via `select` on `ctx.Done()`, so
  cancellation stops new work immediately, active workers exit safely, and
  `wg.Wait()` guarantees cleanup before returning. `ProcessRepository` returns
  `context.Canceled` / `context.DeadlineExceeded` accordingly.
- Cancellation is never mistaken for a per-file failure.

### Structured, non-lossy conflict regions (`internal/git/git.go`)
- New `ConflictRegion` (index, start/end line, ours/theirs/base content, marker
  labels, `HasBase`, `Malformed`, `Incomplete`) and `ConflictParseResult`.
- `ParseConflictRegions` supports standard conflicts, **multiple** regions,
  **adjacent** regions, **diff3** markers (including empty base), **nested**
  markers and **malformed/missing** markers without dropping content.
- `ConflictData` keeps the legacy `BaseVersion`/`OurVersion`/`TheirVersion`
  fields but now derives them from the structured regions and additionally
  exposes `Regions`, `Malformed`, `Incomplete`, `Source` and `StageValidated`.

### Git stage validation
- Staged conflicts require stages **1 (base), 2 (ours), 3 (theirs)**.
- A missing stage returns a typed `*MissingStageError` carrying repository,
  file, stage and underlying Git error. Stage failures are never silently
  ignored.
- Purely inline (non-staged) conflicts are **exempt** from stage validation and
  parsed from disk.

### Semantic identity improvements (`internal/semantic`)
- Identity is now `file | scope | kind | name | signature`
  (`semantic.SymbolIdentity`), with a deterministic fallback to the legacy
  `kind:name` when no richer metadata exists — so existing behaviour is
  preserved while duplicate functions, methods in different classes, nested
  functions and overloads stay distinct.
- `DiffItem` and `SemanticNode` carry an `Identity`; `ComputeConflictScope` and
  graph construction match on it.
- Parser now records `File`, `Scope` (dotted enclosing-scope path) and
  `Signature` (parameter list) per element (`parser.ExtractDataForFile`).

### Run-scoped state isolation (`internal/runstate` — new)
- Replaced the package-level global stores in `graph`, `context` and `cache`
  with a `Run` object owning isolated analysis, prompt-context, graph and report
  maps plus a unique run ID.
- Repeated scans **replace** per-file entries, so graph nodes/edges are never
  duplicated. Every collection is returned deterministically sorted.
- The `internal/cache` package was removed (folded into `Run`).
- The HTTP server now reads from a run instance (`api.StartGraphServer(run, addr)`).

### Deterministic output & error handling
- `ProcessRepository` no longer lets workers write to stdout; outcomes are
  collected and emitted in **sorted file order** by the caller, so output order
  is stable regardless of worker count.
- Repository roots, conflicted files, semantic results, graph nodes/edges and
  API collections are all sorted.
- Per-file failures are collected as structured `*FileError` (with an
  `ErrorCode`); one bad file never aborts the run. `RunResult` reports
  successful files, failed files and a summary. Terminal errors are limited to
  invalid configuration, fatal initialization failure, cancellation and timeout.

### CLI (`cmd/*.go`)
- `analyze` and `resolve` validate configuration first; `resolve` gained
  `--timeout` (default `5m`).
- Commands migrated to `RunE` with `SilenceUsage`/`SilenceErrors` for clean,
  deterministic failures and non-zero exit codes.

---

## 2. New tests

| Area | File | Highlights |
| --- | --- | --- |
| Configuration | `internal/engine/config_test.go` | invalid concurrency/threshold/timeout, boundaries, deterministic first error, error codes |
| Pipeline | `internal/engine/pipeline_test.go` | invalid config is terminal, cancellation, deadline exceeded, per-file error isolation, deterministic order (1 vs 4 workers), repeated-run equivalence, multi-worker race, goroutine-leak check, multi-region preservation, git-stage error mapping |
| Conflict parsing & Git | `internal/git/git_test.go` | standard/multiple/adjacent/malformed/missing-marker/nested/diff3/diff3-empty-base parsing; real-repo staged extraction; **missing stage 1 (add/add)** and **missing stage 2 (delete/modify)**; inline exemption; sorted discovery; cancellation |
| Identity | `internal/semantic/semantic_identity_test.go` | identity parts & fallback, distinct scopes, overloads, distinct node IDs, legacy IDs preserved, precise scope matching |
| Graph | `internal/graph/graph_test.go` | deterministic ordering, no duplicate nodes/edges, root first, repeated-scan stability, merged sorting |
| State | `internal/runstate/runstate_test.go` | unique run IDs, dedup on repeat, sorted collections, run isolation, report scoping/deletion, concurrent registration race |
| Parser | `internal/parser/parser_test.go` | file/scope/signature extraction, nested function scope, method scope, JS signature capture |

Pre-existing `internal/semantic/semantic_test.go` was left intact and still passes
(identity changes are backward compatible).

---

## 3. Coverage summary

`go test -cover ./...`:

| Package | Coverage |
| --- | --- |
| `internal/semantic` | 95.2% |
| `internal/runstate` | 75.0% |
| `internal/graph` | 71.0% |
| `internal/git` | 69.8% |
| `internal/engine` | 64.7% |
| `internal/parser` | 48.7% |

`internal/ai`, `internal/api`, `internal/prompt`, `internal/report` and `cmd`
have no unit tests yet (out of Phase 1 scope).

---

## 4. Remaining known risks

- **AI packages untouched.** `internal/api/generateSuggestions` still hard-codes
  Ollama/`qwen2:1.5b`, and AI requests still use `context.Background()`; the
  resolver-level timeout/retry/schema/confidence work belongs to Phase 2.
- **Report deletion after AI resolution is preserved** (critical issue #16). The
  pipeline still deletes the cached report once AI resolution runs; this is an
  AI-workflow concern and was intentionally left to a later phase.
- **`PrintPayloadJSON` still prints only a heading** (#15); not part of the
  Phase 1 scope.
- **Server concerns** (#7, #8, #9: static path resolution, method validation,
  request limits, configurable CORS, graceful shutdown, health/readiness
  endpoints) remain for the API/server phase.
- **`history.jsx` still calls the removed `/api/prompt/statistics`** (#1) — a
  frontend/API-phase item; no endpoint was re-added in Phase 1.
- **Nested conflict markers** are preserved and flagged `Malformed` but a nested
  closing marker will close the outer region; the raw content is retained so no
  information is lost.
- Test fixtures in `conflicts/` were **not modified**.

---

## 5. Migration notes

- `engine.Config` gained `Timeout time.Duration`; construct configs via
  `engine.DefaultConfig()` or set a positive timeout, or `Validate()` fails.
- Internal signatures changed:
  - `engine.FindConflicts(ctx, scanRoot)`
  - `engine.ProcessRepository(ctx, run, repoRoot, files, cfg, runAI) (*RunResult, error)`
  - `engine.ProcessConflictFile(ctx, run, repoRoot, file, cfg, runAI) FileOutcome`
  - `git.FindGitRepositoryRoots(ctx, root)`, `git.GetConflictedFiles(ctx, repo)`,
    `git.ExtractConflictVersions(ctx, repo, file)`
  - `api.StartGraphServer(run, addr)`
- Global store helpers removed: `graph.RegisterGraph`, `graph.MergedGraph`,
  `graph.MergedGraphDTO`, `promptcontext.RegisterAnalysis`/`RegisterPromptContext`
  (and getters), and the whole `internal/cache` package. Use `runstate.Run`.
- `RunResult` replaces the bare `[]FileOutcome` return so callers can report
  successful vs failed files.
- API response **shapes are unchanged** (additive `identity`/`malformed` fields
  only), so the frontend contract is preserved.

---

## 6. Recommended Phase 2 starting point

Begin **"Build Ollama-first AI"** by first closing issue #1 so the existing
History page is not broken while AI work proceeds:

1. Centralize provider/model configuration (remove the hard-coded
   Ollama/`qwen2:1.5b` in `internal/api/server.go`), thread the run `context`
   into AI calls, and add Ollama health/model checks.
2. Add per-request timeouts, bounded retries, strict response-schema and
   confidence validation, and bounded collision-level concurrency.
3. Either restore a token-statistics/history endpoint or replace the History
   page with real run/audit history now that runs are first-class (`runstate.Run`).

Phase 2 should reuse `runstate.Run` for resolution IDs, patches, source ranges,
confidence and approval/validation state.

---

## 7. Phase 1 finalization (follow-up review)

An independent review closed six remaining gaps. All are now fixed and verified
with `go test ./...`, `go test -race ./...` and `go vet ./...`.

1. **Error propagation.** `engine.FindConflicts` and `git.GetConflictedFiles` no
   longer swallow errors. `git ls-files`, `filepath.WalkDir` and file reads all
   propagate: a repository failure can never be reported as a successful scan,
   and a Git failure stays distinguishable from "no conflicts found".
2. **Filesystem cancellation.** `GetConflictedFiles` checks the context before
   and after Git operations and before/after every filesystem operation;
   traversal stops on cancellation, `ctx.Err()` is never discarded, and
   cancellation returns `context.Canceled`/`context.DeadlineExceeded`, never a
   successful result.
3. **Repository-scoped run state.** Every collection in `runstate.Run`
   (analyses, prompt contexts, graphs, reports, suggestions) is keyed by
   `runstate.FileKey(repoRoot, file)` = `repoRoot + "|" + file`, and graph root
   IDs are repository-safe (`graph.BuildCyGraph(repoRoot, file, ...)`). Identical
   relative paths in different repositories no longer overwrite or collide.
4. **Semantic scope traversal.** `ComputeConflictScope` seeds from *all* symbol
   kinds (functions, methods, constructors, classes, variables, files and
   unknown future kinds), matching the full identity first and falling back to
   the legacy `kind:name` key only for un-identified elements; incoming and
   outgoing edges are traversed deterministically.
5. **Deterministic semantic output.** Ordering uses the exact tie-breaker
   sequence `identity -> file -> kind -> name -> line -> type`
   (`semantic.CompareDiffItems`/`SortDiffItems`), applied to collisions,
   changes and graph-derived semantic collections. Repeated runs, worker counts
   and map iteration order no longer affect output.
6. **Global suggestion cache removed.** The package-level
   `suggestionCache`/`suggestionMu` and `repoMetadata` globals in
   `internal/api` are gone; suggestions and repository metadata now live on
   `runstate.Run` under repository-safe keys.

### Global mutable state audit

Remaining package-level mutable variables and their justification:

| Variable | Location | Justification |
| --- | --- | --- |
| `providers`, `registryMutex` | `internal/ai/ai.go` | Write-once provider registry populated at process init; guarded, static configuration, not run data. |
| `conflictedFileExtensions` | `internal/git/git.go` | Read-only lookup table; effectively a constant. |
| `debugLoggingEnabled` | `internal/engine/pipeline.go` | Process-wide diagnostic toggle set once from CLI flags. |
| `rootCmd`, `*Cmd` | `cmd/*.go` | Cobra command tree constructed at init. |
| flag binding vars (`port`, `provider`, `modelName`, ...) | `cmd/*.go` | Cobra flag targets bound once at startup. |

No package-level mutable state remains for suggestions, reports, analyses,
graphs, prompt contexts or caches.

### Test coverage added in the finalization

- `internal/git/discovery_test.go`: non-repository surfaces a `*GitError`;
  empty repo is not an error; broken-symlink and (non-root) unreadable-file read
  failures propagate; discovery cancellation.
- `internal/engine/discovery_test.go`: `FindConflicts` surfaces a Git failure and
  never treats cancellation as success; multi-repository pipeline isolation
  (identical relative paths produce two analyses and two graph roots).
- `internal/runstate/isolation_test.go`: `FileKey` scoping, identical filenames
  in different repositories, prompt-context scoping, repository-safe graph IDs,
  merged-graph determinism across registration order, suggestion and metadata
  run/repository isolation.
- `internal/semantic/scope_kinds_test.go`: class, method, constructor, variable
  and unknown-kind collisions; legacy fallback matching.
- `internal/semantic/determinism_test.go`: repeated-run equivalence, stable
  tie-breaker ordering, total-order verification.

Two pre-existing expectations in `internal/semantic/semantic_test.go` were
updated (not deleted) to match the new required behavior: variable collisions
now seed the conflict scope, and change ordering is by identity rather than by
status.
