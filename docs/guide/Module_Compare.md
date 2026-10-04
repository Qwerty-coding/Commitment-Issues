# Module: Compare

## Purpose

This package implements the roadmap's Phase 5 feature (the API layer
labels its endpoints "Phase 4B" in code comments): hypothetical merge
analysis of two commits or two branches (optionally across two repositories) **without
ever touching the working tree or index**. It classifies every changed
file into a precise 3-way status, detects renames, and — when both sides
changed a source file — runs the same AST + semantic diff as the main
pipeline to distinguish *structural* collisions (same symbol changed on
both sides) from mere content conflicts.

## Files

### `backend/internal/compare/compare.go`

#### Primary Role
The `CompareCommits` entry point, rename-aware logical file mapping,
per-file 3-way classification, and result summarization.

#### Key Structures
- `FileStatus` constants: `ours_only`, `theirs_only`, `identical`,
  `structural_collision`, `content_conflict`, `add_add_conflict`,
  `delete_modify_conflict`, `rename_conflict`, `binary_conflict`,
  `unsupported_language`, and `analysis_error` (`StatusError` — an
  explicit failure entry, never a silent "clean" result).
- `Options{Repo1, Repo2, BaseRef, OursRef, TheirsRef, Cache
  cache.Cache}` — `Repo2` empty means same-repo comparison; a nil `Cache`
  gets a per-comparison `cache.NewDefault()` (cache state is never shared
  across comparisons unless explicitly injected).
- `FileComparison{Path, OldPath, Status, Explanation, Recommendation,
  BaseHash, OursHash, TheirsHash, IsBinary, IsSupported, SmartDiff
  *semantic.SmartDiffResult}` — one file's verdict.
- `Result{Repo1, Repo2, BaseRef, OursRef, TheirsRef,
  MergeBaseResolved, UnrelatedRepositories, Files, Summary}` and
  `Summary` — per-status counters incl. `RenameConflicts` and `Errors`.
- Internal `fileVersions{displayPath, basePath, oursPath, theirsPath,
  renamedOnOurs, renamedOnTheirs, divergentRename}` — one *logical* file
  whose concrete path may differ per side.
- `fileError(fv, format, ...)` — builds a `StatusError` entry with a
  fixed recommendation.

#### Algorithms & Logic
- **`CompareCommits`** runs fixed stages:
  1. Resolve ours/theirs refs (`git.ResolveRef`) and an explicit base if
     given; else same-repo → `git.FindMergeBase`
     (`ErrNoMergeBase` → `unrelated = true`, not an error) → cross-repo →
     `git.CheckRepositoriesSharedHistory` (no shared ancestry →
     unrelated). Any other git failure is terminal.
  2. Enumerate changed files: with a base, union of `git.DiffTrees`
     base↦ours and base↦theirs + `git.DetectRenames` on both sides; when
     unrelated, full `git.ListCommitFiles` per side (full-tree
     comparison).
  3. `sort.Strings` for deterministic ordering →
     `buildLogicalFiles(...)`.
  4. Per logical file: `compareFile` (below), tallied into `Summary`
     with `TotalFiles` set at the end. Cancellation checked per file.
- **`buildLogicalFiles`**: rename sources (old paths from either side's
  rename map) become single logical entries — `basePath = old`,
  per-side `oursPath`/`theirsPath` set to the rename target; both sides
  renaming to *different* targets sets `divergentRename` (display path =
  the base path); the rename's new paths and the base path are marked
  `handled` so raw diff paths don't re-appear as phantom add/delete
  entries; remaining plain paths pass through; entries sorted by display
  path. Result: a rename is analyzed as ONE file, never delete+add.
- **`compareFile`** — ordered decision tree:
  1. `OldPath` recorded when renamed; `divergentRename` →
     `rename_conflict` immediately (content comparison across different
     targets would mislead).
  2. Blob presence/identity via `git.FileHashAtCommit` for
     ours/theirs/base — `ErrFileNotInTree` is *expected data*
     (add/delete); any other failure → `analysis_error`.
  3. Hash-based classification: all present & equal → `identical`;
     ours==base → `theirs_only`; theirs==base → `ours_only`; no base &
     both present → equal ? `identical` : `add_add_conflict`.
  4. Rename/delete combinations checked **before** delete/modify (a
     rename on one side + delete on the other is `rename_conflict`, not
     plain delete/modify), then delete/modify (untouched side → clean
     single-sided verdict), then single-sided adds.
  5. Content read via `git.ShowFileAtCommit` (read-only); binary sniff
     (`fileutil.IsBinaryContent`) → `binary_conflict`; unsupported
     extension → `unsupported_language` (text fallback noted).
  6. AST stage: three `astCache.GetOrParse` calls (soft errors
     `ErrFileTooLarge`/`ErrBinaryFile` tolerated) →
     `semantic.GenerateSmartDiff` → collisions present →
     `structural_collision` with the colliding symbol names (plus a
     rename note); no collisions → `content_conflict`.
  Every explanation/recommendation is deterministic and includes the
  rename note (`ours renamed X to Y`, etc.) where applicable.

#### Wiring (Dependencies)
- Imports: `internal/cache` (AST cache), `internal/fileutil` (binary
  sniff), `internal/git` (all read-only plumbing), `internal/parser`
  (`IsSupportedLanguage`, soft-error sentinels), `internal/semantic`
  (smart diff).
- Imported by: `cmd/compare.go` (CLI, including `--json` output and
  `errors.Is(git.ErrNoMergeBase)` exit classification) and
  `internal/api/compare.go` (the `/api/compare` endpoint); tests in the
  package.

## Cross-Module Flow

- `cmd/compare` → `compare.CompareCommits(Options{...})` → git plumbing
  (`ResolveRef` → merge base → `DiffTrees`/`DetectRenames` →
  `FileHashAtCommit`/`ShowFileAtCommit`) → cache-backed parser →
  semantic diff → `Result` → human summary or `--json`.
- API path: `POST`/`GET /api/compare` (in `internal/api/compare.go`)
  builds the same `Options` from query/body parameters and returns
  `Result` as JSON for a future Compare UI.
- Invariants re-used from the main pipeline: same cache keying, same
  soft-error handling, same deterministic ordering; zero working-tree
  mutation is guaranteed because only `git show`/`ls-tree`/`diff`
  reads are used.

## Tests

- `backend/internal/compare/compare_test.go` — classification matrix for
  the status set, rename handling (incl. `TestCompare_RenameConflict`),
  merge-base edge cases (`TestCompare_SameRepo_MissingMergeBase`,
  `TestCompare_MultipleRepositories_SharedHistory`), orphan/missing-file
  behavior, and `TestCompare_ZeroWorkingTreeMutations` (asserts the
  working tree and index are untouched after comparison).
