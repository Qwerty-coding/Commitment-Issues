# 01 — Execution Flow

This document traces how the system actually runs: entry points, the core
loops step by step, and one concrete end-to-end data-flow example.

## Entry Points

### Backend — `backend/main.go`

```go
func main() { cmd.Execute() }
```

`cmd.Execute()` (in `backend/cmd/root.go`) runs the cobra root command
(`SilenceUsage`, `SilenceErrors` for machine-parseable failures) and exits
with code 1 on error. Five subcommands register themselves in `init()`:

| Command | AI? | Purpose |
|---|---|---|
| `scan [path]` | No | List conflicted files per repository |
| `analyze [path]` | No | Full AST/diff analysis + JSON reports |
| `resolve [path]` | **Yes** | `analyze` + per-collision AI resolutions |
| `serve --port :8080 [path]` | No* | Analyze, then start the HTTP graph/API server |
| `compare --repo R --ours X --theirs Y [--base B] [--json]` | No | Hypothetical merge analysis of two commits/branches |

\* `serve` never invokes a resolver itself; the `/api/suggestions` endpoint it
hosts *does* invoke AI on request.

### Frontend — `frontend/src/main.jsx`

Mounts `App.jsx`, which wraps routes in `BrowserRouter`: `/` (dashboard),
`/conflicts`, `/treediff`, `/suggestions`, `/commitgraph`, `/history`,
`/context`. Every tab fetches `GET {VITE_API_BASE}/api/...` from the backend
(same origin in production — the Go server serves `frontend/dist`).

---

## Core Loop 1 — `scan` / `analyze` / `resolve` (shared pipeline)

All three commands run the same orchestration; they differ only in the
`runAI` flag and configuration.

```text
main → cmd.Execute → {scan|analyze|resolve}Cmd.RunE
```

**Step 1 — Configuration.**
`engine.DefaultConfig()` builds the base config (concurrency, timeout,
validation allow-list, `ASTCache: cache.NewDefault()`, AI defaults).
`resolve` additionally assembles AI config with precedence
**flags > env (`AI_*`) > provider defaults** (`buildAIConfigFromFlags`,
re-applying provider defaults when `--provider` switches) and the README
options (`--readme-context` / `--readme-max-bytes`, default on / 4096 bytes).
`cfg.Validate()` and `cfg.AI.Validate()` run **before** any scanning.

**Step 2 — Scan root resolution.**
`engine.ResolveScanRoot(targetPath)` resolves the user path against the
working directory (falling back to the repo root of the backend module).

**Step 3 — Conflict discovery.**
`engine.FindConflicts(ctx, scanRoot)`:
1. `git.FindGitRepositoryRoots` walks the tree for `.git` directories
   (deterministically sorted; subdirectory scans are filtered to the scan
   root).
2. Per repo, `git.GetConflictedFiles` unions two sources:
   - `git ls-files -u -z` → files with unmerged index entries (staged
     conflicts),
   - a filesystem walk over known source extensions parsing files with
     `git.ParseConflictRegions` → inline-marker conflicts (fixtures).
   Any git/fs failure is a terminal error — never "no conflicts".

**Step 4 — Per-repository processing.**
`runstate.NewRun()` creates the run-scoped state container. Then per repo:
`engine.ProcessRepository(ctx, run, repoRoot, files, cfg, runAI)`:
- creates one AST cache if `cfg.ASTCache == nil` (per-run, never global),
- loads the README excerpt once (when `cfg.ReadmeContext`) via
  `promptcontext.LoadReadmeContext(repoRoot, cfg.ReadmeMaxBytes)` and stores
  it repo-scoped on the run,
- fans out `ProcessConflictFile` across a semaphore-bounded worker pool
  (`cfg.MaxConcurrency`, cancellation-aware), collecting per-file outcomes —
  one bad file never kills the run.

**Step 5 — Per-file pipeline** (`engine.ProcessConflictFile`, the heart of
the system):

1. **Version extraction** — `git.ExtractConflictVersions(repoRoot, file)`:
   staged files read index stages 1/2/3 via `git show :N:path`
   (`MissingStageError` if any stage absent); inline files parse markers
   into structured `ConflictRegion`s (diff3, nested, malformed, incomplete
   all preserved) and reconstruct full-file ours/theirs/base views.
2. **AST extraction (cached)** — for supported languages, each version goes
   through `ASTCache.GetOrParse(repo, path, source)`:
   content hash → cache lookup → miss → `parser.ParseAndExtract`, which
   guards size (10 MB) and binary content, parses with the grammar selected
   from the extension (`tiers.go` registry → `GetLanguageForFile`), runs the
   per-language extractor `extractLanguageData` (functions, methods with Go
   receiver scoping, classes/structs/interfaces/enums/traits, variables,
   imports, calls, HTML elements), collects syntax `Diagnostics`, and stamps
   `Language`, `ParserName`, `ParserVer`, `ContentHash`. The immutable
   result is cached and cloned to the caller.
   Unsupported languages skip AST with a visible "text fallback" note.
3. **Smart diff** — `semantic.GenerateSmartDiffContext(base, ours, theirs)`
   classifies every symbol: `Collisions` (changed on both sides),
   `OurChanges`, `TheirChanges` — with stable identities
   (`file+scope+kind+name+signature`) so overloads and scoped methods never
   merge.
4. **Scope + prompt context** — ours/theirs semantic graphs are built and
   merged; `ComputeConflictScope` finds the enclosing scopes of collisions;
   `promptcontext.BuildPromptContext` assembles the IR: repository summary,
   **README excerpt** (from step 4), files, collision-scoped functions with
   language-correct code fences, merged deduped imports, and the rendered
   context text + token estimate.
5. **State + report registration** — the run records the prompt context,
   Cytoscape graph (`graph.BuildCyGraph`), and a full `FileAnalysis`
   (ASTs, SmartDiff, working-tree content hash, conflict regions,
   per-region context hashes, parser version, timestamp). The payload JSON
   (`prompt.MarshalAIRequestPayload`, a compact TOON-style encoding) is
   rendered into the CLI report and cached on the run — **retained** as part
   of the audit trail.
6. **Optional AI resolution** (`runAI == true`, i.e. `resolve`):
   `runAIResolution` health-checks the provider (`ai.CheckProvider` —
   Ollama reachability + model presence, API-key presence for hosted
   providers), then per collision:
   `ai.ResolveCollisionWithRetry` enforces the prompt-size cap, then loops
   `attempt ≤ RetryCount`: per-attempt timeout → `Resolver.ResolveCollision`
   (system prompt + `promptCtx.Context` + collision JSON → strict
   `{"explanation","suggested_code","confidence_score"}` schema validation,
   fenced-JSON tolerant) → retry only on typed *retryable* transport/HTTP
   errors with exponential backoff; cancellation never retries.
   Every outcome is recorded: a `runstate.Suggestion` (stable ID, provider,
   model, status incl. `below_threshold` / `manual_review` / `failed`,
   timing, typed error) and, on success, a `resolutions.Resolution` in
   `StatusProposed` with region identity + content hashes for later
   application.

**Step 6 — Summary.** Each command prints per-file output and
`result.Summary()`; non-zero file failures exit non-zero.

---

## Core Loop 2 — `serve`

```text
serveCmd → DefaultConfig (+ env AI metadata) → FindConflicts → NewRun
        → SetRepositoryMetadata (name, current/incoming branch)
        → ProcessRepository(..., runAI=false) per repo
        → runstate.NewHistory(100) + record RunHistory entry
        → api server on --port (serves /api/* and frontend/dist SPA)
```

The server (`internal/api/server.go` + `resolutions.go`) then answers:

- `GET /api/repository` — repo metadata
- `GET /api/graph` (+ `/expand`, `/collapse`, `/focus`) — semantic graph
- `GET /api/analysis[?file=]` — `FileAnalysis` snapshots
- `GET /api/prompt[?file=]` — prompt contexts (incl. README excerpt)
- `GET /api/suggestions[?file=]` — **AI-invoking** on-demand suggestions
  with `meta` (provider, model, run ID, partial failures)
- `GET /api/history` — bounded run history
- `GET /api/resolutions[...]` + `POST /api/resolutions/{id}/preview|approve|apply|revert`
  + `GET /api/resolutions/{id}/validation` + `POST /api/analysis/refresh` —
  the gated mutation surface
- `GET|POST /api/compare` — hypothetical commit/branch comparison
  (`internal/api/compare.go`, analysis-only; typed ref/merge-base errors)
  with a scaffolded but currently empty `/compare` frontend route

**Mutation trace (the only write path):**
`suggestions.jsx` button → `POST /api/resolutions/{id}/preview` (read-only
unified diff built by `internal/patch`) → `POST .../approve` (recorded;
expires on file change or regeneration) → `POST .../apply`:
1. `internal/safeguard` re-reads the working file and verifies the
   analysis-time content hash + region-context hash (stale → typed
   `STALE_FILE` error, must re-analyze), repository identity, and path
   containment (`fileutil.AbsFilePath` rejects escapes),
2. `internal/apply` snapshots the exact pre-apply bytes, then
   `fileutil.AtomicWrite` (temp file + rename) applies the replacement for
   the *one* approved region only,
3. `internal/validation` runs only user-allow-listed commands
   (formatters/tests), capturing bounded output, exit code, duration;
   failure marks `validation_failed` — never auto-reverts.
`POST .../revert` verifies the current content matches the post-apply hash
(refusing to clobber unrelated user edits) and restores the snapshot
atomically. Every transition is an immutable audit event on the run.

---

## Core Loop 3 — `compare` (hypothetical merge analysis)

```text
compareCmd → compare.CompareCommits(ctx, Options{Repo1[, Repo2], BaseRef, OursRef, TheirsRef[, Cache]})
```

1. **Ref resolution** — `git.ResolveRef` (`rev-parse --verify <ref>^{commit}`)
   for ours/theirs (and explicit base if given).
2. **Merge base** — same repo: `git.FindMergeBase`; two repos:
   `git.CheckRepositoriesSharedHistory` probing with
   `GIT_ALTERNATE_OBJECT_DIRECTORIES` (read-only). Exit code 1 →
   `ErrNoMergeBase` → result is marked `UnrelatedRepositories` and falls
   back to full-tree comparison; any other git failure is a terminal error.
3. **Change enumeration** — `git.DiffTrees` base↦ours and base↦theirs (or
   full `ListCommitFiles` when unrelated), unioned and sorted;
   `git.DetectRenames` (`-M --name-status`) on both sides.
4. **Logical files** — `buildLogicalFiles` folds renames into single logical
   entries with per-side paths (`base/ours/theirs` may differ), so a rename
   is never misreported as delete+add; divergent renames (same base → two
   targets) become `rename_conflict`.
5. **Per-file 3-way analysis** — `compareFile` reads blob hashes
   (`FileHashAtCommit`; absence is `ErrFileNotInTree`, a first-class case)
   and classifies: identical / ours-only / theirs-only / add-add /
   delete-modify / rename / binary / unsupported. Files changed on both
   sides get contents via `git.ShowFileAtCommit` (never touching the working
   tree), AST-parse through the same cache, and
   `semantic.GenerateSmartDiff` → `structural_collision` with collision
   names, or `content_conflict`. Any unexpected read/parse failure becomes
   an explicit `analysis_error` entry — never a silent "clean".
6. **Result** — sorted `FileComparison`s + `Summary` counts (incl.
   `renameConflicts`, `errors`); CLI prints human summary or `--json`.

---

## Concrete Data Flow — one conflicted Python file through `resolve`

```text
conflicts/sample2.py (inline <<<<<<< markers)
  └─► git.ExtractConflictVersions          → ConflictData{Regions, Base/Ours/Theirs, Source:inline}
      └─► cache.GetOrParse ×3 versions      → parser.ASTContext ×3 (OrderProcessingEngine class,
      │                                        9 methods, imports, calls, diagnostics)
      ├─► semantic.GenerateSmartDiffContext → SmartDiffResult{Collisions, OurChanges, TheirChanges}
      ├─► semantic graph + conflict scope   → SemanticGraph (merged ours+theirs)
      ├─► promptcontext.BuildPromptContext  → PromptContextIR{RepositoryReadme, Files,
      │                                        Functions(scoped), Imports, Context, EstimatedTokens}
      ├─► prompt.MarshalAIRequestPayload    → TOON-style text payload (CLI report + run report)
      ├─► run.Register{PromptContext,Graph,Analysis} + SaveReport
      └─► ai.ResolveCollisionWithRetry per collision
            system prompt + Context + collision JSON ──► Ollama/Gemini/Groq
            ◄── {"explanation","suggested_code","confidence_score"}
          → runstate.Suggestion{Status: complete|below_threshold|failed, Provider, Model, ...}
          → resolutions.Resolution{Status: proposed, ContentHash, ContextHash, RegionID}
            (later: preview → approve → apply → validate → revert via /api/resolutions/*)
```

## Termination & Invariants

- CLI commands exit 0 on success, 1 on any structured failure; partial file
  failures are reported but only fatal-as-a-count at the end.
- The server runs until interrupted; history/state is bounded in-memory and
  dies with the process (by design).
- Invariants held on every path: deterministic ordering, run-scoped state,
  no working-tree mutation outside `apply`, no AI outside `resolve` +
  `/api/suggestions`, typed errors with redaction, context cancellation
  never swallowed.
