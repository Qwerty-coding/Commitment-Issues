# Module: Engine

## Purpose

The engine package is the orchestrator: it resolves scan roots, discovers
conflicted files per repository, runs the bounded worker-pool analysis
pipeline for each file, optionally invokes AI resolution, and expresses
every failure as a structured, coded error. It wires together *every*
read-side module (git, parser, semantic, context, prompt, graph, report,
cache, runstate) and the AI layer — it is the heart of the system.

## Files

### `backend/internal/engine/pipeline.go`

#### Primary Role
Scan-root resolution, conflict discovery, repository-level parallel
processing, the per-file analysis pipeline, and AI resolution recording.

#### Key Structures
- `FileOutcome{File, Output, ReportPath, Err}` — per-file result; `Err`
  is always a structured `*FileError` (or context error), so one bad
  file never terminates a run.
- `RunResult{RunID, Repository, Duration, Outcomes, Succeeded, Failed}`
  + `Summary()` — deterministic human-readable run summary (file order
  preserved from the sorted input).
- Debug logging: `EnableDebugLogging`/`debugPrintf` — the package-level
  `debugLoggingEnabled` flag is write-once startup config (toggled by
  `--debug` before any work), not per-run state.
- Discovery: `ResolveScanRoot(argPath)` (absolute → cwd-relative →
  parent-of-cwd fallback when `go.mod` marks the module dir; empty arg →
  module dir), `FindConflicts(ctx, scanRoot) (map[repoRoot][]files,
  sortedRepoRoots, err)`.
- Processing: `ProcessRepository(ctx, run, repoRoot, files, cfg, runAI)`,
  `ProcessConflictFile(ctx, run, repoRoot, file, cfg, runAI)`,
  `runAIResolution(...)` (unexported), `recordAISetupFailures(...)`,
  `readmeExcerpt(run, repoRoot)`, `runID`, `formatDuration`.

#### Algorithms & Logic
- **`FindConflicts`**: `git.FindGitRepositoryRoots` → per repo
  `git.GetConflictedFiles`; any discovery failure is returned (never
  misreported as "no conflicts"); when `scanRoot` is *inside* a repo, the
  file list is filtered to the scan-root prefix (`filepath.Rel` +
  separator-aware `HasPrefix`); files and repo roots are sorted.
- **`ProcessRepository`**: validates config first (fixed-order
  `cfg.Validate()`), ensures a per-run AST cache (`cfg.ASTCache == nil`
  → `cache.NewDefault()`; config is by-value so this stays local — no
  package globals), loads the README **once per repository** before the
  pool (`promptcontext.LoadReadmeContext` → `run.RegisterReadme`; errors
  are non-fatal debug logs; feature-disabled registers `""` so workers
  read uniformly), then fans out one goroutine per file through a
  semaphore of `cfg.MaxConcurrency` with cancellation-aware acquisition
  (`select sem<- / ctx.Done()` — no blocked workers on cancel), collects
  outcomes by index (deterministic order), splits succeeded/failed, and
  returns `(result, ctx.Err())` on cancellation.
- **`ProcessConflictFile`** — the core pipeline:
  1. `git.ExtractConflictVersions` → typed errors mapped to codes
     (`*git.MissingStageError` → `CodeGitStage`, else `CodeFileExtract`).
  2. Language check (`parser.GetLanguageForFile`); three cached parses
     via `astCache.GetOrParse` (ours, theirs, *base*) — ours/theirs
     failures are `CodeFileParse`, a base failure is logged and skipped
     (empty base is legitimate for add/add); unsupported language →
     visible text-fallback note.
  3. `semantic.GenerateSmartDiffContext` → `BuildSemanticGraph` ×2 →
     `MergeSemanticGraphs` → `ComputeConflictScope`.
  4. `promptcontext.BuildPromptContext(repoSummary, readmeExcerpt,
     files, conflictScope, ourAST, theirAST)` — the README excerpt comes
     from the run (`readmeExcerpt`).
  5. `prompt.MarshalAIRequestPayload` → rendered into the buffer via
     `report.PrintPayloadJSON`; `report.PrintASTContext`/`PrintDiffReport`
     build the human output.
  6. Run-scoped registration (all keys repo-qualified):
     `RegisterPromptContext`, `RegisterGraph(graph.BuildCyGraph(...))`,
     Phase-2 safety metadata — `fileutil.RegionContextHashesForFile`
     (content hash + per-region context hashes) and conflict regions
     (inline regions as-is, or re-parsed from the working-tree copy for
     staged files) — into `RegisterAnalysis` as a
     `promptcontext.FileAnalysis` with `ParserVersion` and UTC timestamp.
  7. `run.SaveReport` retains the payload JSON as audit trail (kept even
     after AI resolution, deliberately).
  8. If `runAI`: `runAIResolution`.
- **`runAIResolution`**: syncs `cfg.AI.ConfidenceThreshold` from the
  engine config (one source of truth), `ai.GetResolver` + health
  `ai.CheckProvider` — failures record a **typed failed Suggestion for
  every collision** (`recordAISetupFailures`) and return nil (the run
  continues); then per collision (cancellation checked first):
  `ai.ResolveCollisionWithRetry` → region binding via
  `git.MatchConflictRegion(analysis.ConflictRegions, ...)` →
  `runstate.Suggestion` with `SuggestionKey(repo, file, DiffKey)` identity
  and status `StatusFailed` (typed `ai.AsError` code + retryable flag) /
  `StatusComplete` / `StatusBelowThreshold` (confidence < threshold); on
  success a `resolutions.Resolution` in `StatusProposed` is saved with
  region bounds, content/context hashes, and replacement code, and its ID
  is linked on the suggestion. Per-collision AI errors are printed and
  *skipped*, not fatal; cancellation always returns.

#### Wiring (Dependencies)
- Imports: `internal/ai`, `internal/cache`, `internal/context`
  (promptcontext), `internal/fileutil`, `internal/git`, `internal/graph`,
  `internal/parser`, `internal/prompt`, `internal/report`,
  `internal/resolutions`, `internal/runstate`, `internal/semantic`.
- Imported by: `cmd/scan.go`, `cmd/analyze.go`, `cmd/resolve.go`,
  `cmd/serve.go` (all orchestration entry points) and
  `internal/api` (analysis refresh path).

### `backend/internal/engine/config.go`

#### Primary Role
Structured error taxonomy and runtime configuration for the pipeline.

#### Key Structures
- `ErrorCode` constants: `CodeInvalidConfig` (INVALID_CONFIG),
  `CodeCancelled` (CANCELLED), `CodeTimeout` (TIMEOUT), `CodeGitStage`
  (GIT_MISSING_STAGE), `CodeFileParse` (FILE_PARSE_ERROR),
  `CodeFileExtract` (FILE_EXTRACT_ERROR), `CodeInternal`
  (INTERNAL_ERROR).
- `CodedError` interface (`error` + `Code()`); `ErrorCodeOf(err)` defaults
  to `CodeInternal`.
- `ConfigError{Field, Value, Rule, Message}` — one field at a time in a
  fixed, documented order (deterministic validation messages).
- `FileError{File, ErrCode, Err}` — `Unwrap` + `Code()`; represents
  per-file failures that don't kill the run.
- `DefaultReadmeMaxBytes = 4096`; `EnvTargetPromptTokens =
  "AI_TARGET_PROMPT_TOKENS"`.
- `Config{MaxConcurrency, ConfidenceThreshold, Timeout, AI ai.Config,
  Validation validation.Config, ASTCache cache.Cache, ReadmeContext,
  ReadmeMaxBytes, AITargetPromptTokens}` — AI settings live solely in the
  shared `ai.Config` (flags > env > provider defaults); analysis-only
  fields govern the pipeline. `AITargetPromptTokens` is the soft prompt
  budget applied per conflict file (`0` disables); the trim order is
  README excerpt → functions → variables and the collision payload is
  never trimmed.
- `DefaultConfig()` — concurrency 4, threshold 70, timeout 5m,
  `ai.Default("")`, `DefaultASTCache()`, README enabled with 4096 bytes,
  prompt budget 8000 tokens; applies `AI_README_CONTEXT` (boolean-ish via
  `equalsIgnoreCase` "false"/"0"/"no"/"off"), `AI_README_MAX_BYTES`
  (positive int) and `AI_TARGET_PROMPT_TOKENS` (>= 0) overrides.
- `DefaultASTCache()` — builds the AST cache from the `CACHE_*`
  environment variables (`CACHE_DISK_ENABLED`, `CACHE_DIR`,
  `CACHE_MAX_DISK_BYTES`, `CACHE_MAX_ENTRIES`,
  `CACHE_MAX_MEMORY_BYTES`); disk cache is opt-in and falls back to a
  safe in-memory cache when the configured directory is unusable.

#### Algorithms & Logic
`Validate()` runs checks in fixed order — `MaxConcurrency >= 1` (a
non-positive value would deadlock the processing semaphore), `Timeout > 0`,
`0 <= ConfidenceThreshold <= 100` — and must be called *before* scanning
so invalid settings never start analysis.

#### Wiring (Dependencies)
- Imports: `internal/ai` (config type + env names), `internal/cache`,
  `internal/validation`, stdlib (`os`, `strconv`, `strings`, `time`).
- Imported by: everything in the package plus `cmd/*` and
  `internal/api` via `engine.Config`.

## Cross-Module Flow

- `cmd/analyze|resolve|serve` → `ResolveScanRoot` → `FindConflicts` →
  `runstate.NewRun` → `ProcessRepository` (per repo) →
  `ProcessConflictFile` (per file) → registered state on the run →
  `RunResult.Summary()`.
- Per file: `git.ExtractConflictVersions` → `cache.GetOrParse` →
  `semantic.*` → `promptcontext.BuildPromptContext` (+ README) →
  `prompt.MarshalAIRequestPayload` → `runstate` registrations →
  optional `ai.ResolveCollisionWithRetry` → `runstate.Suggestion` +
  `resolutions.Resolution`.
- Failures: any layer's error is wrapped as `*engine.FileError` with a
  stable code; `cmd` prints it, the API maps it.

## Tests

- `backend/internal/engine/pipeline_test.go` — pipeline behavior
  (extraction failures, base-parse tolerance, report registration,
  cancellation).
- `backend/internal/engine/discovery_test.go` — `FindConflicts`/
  `ResolveScanRoot` edge cases (subdirectory filtering, error
  propagation).
- `backend/internal/engine/config_test.go` — `Validate()` order and
  messages, defaults, README env overrides.
- `backend/internal/engine/ai_resolution_test.go` — AI path: suggestion
  statuses, setup-failure recording, threshold behavior.
