# Module: Git

## Purpose

Every interaction with Git lives here: discovering repositories, finding
conflicted files (both staged index conflicts and on-disk inline conflict
markers), extracting the three versions of a conflicted file losslessly,
branch metadata, and the read-only plumbing used by commit/branch
comparison (refs, merge base, tree diffs, blob hashes, rename detection).
The package only ever *reads* — it never mutates the index, working tree,
or history.

## Files

### `backend/internal/git/git.go`

#### Primary Role
Conflict representation, repository discovery, conflicted-file discovery,
inline-marker parsing, and three-way version extraction.

#### Key Structures
- `ConflictRegion{Index, StartLine, EndLine, Ours, Theirs, Base, HasBase,
  OursLabel, TheirsLabel, BaseLabel, Malformed, Incomplete}` — one
  structured, non-lossy inline region (labels are the text after the
  marker, e.g. branch names).
- `ConflictParseResult{Regions, OursText, TheirText, BaseText, Malformed,
  Incomplete}` — full parse of a file, including reconstructed whole-file
  ours/theirs/base views.
- `ConflictData{FileName, BaseVersion, OurVersion, TheirVersion, Regions,
  Malformed, Incomplete, Source, StageValidated}` — pipeline payload;
  `Source` is `SourceStaged` ("staged") or `SourceInline` ("inline");
  legacy version fields are always derived from `Regions` so nothing is
  lost.
- Typed errors: `MissingStageError{Repository, File, Stage, Err}`
  (stage 1=base, 2=ours, 3=theirs; `Unwrap` supported) and
  `GitError{Operation, Dir, Args, Err, Stderr}` — structured wrapper for
  failed `git` invocations with stderr captured.
- Functions: `FindGitRepositoryRoots` (deterministically sorted walk for
  `.git` dirs), `GetConflictedFiles`, `listUnmerged`
  (`git ls-files -u -z` → filename → set of stages), `CurrentBranch`
  (`symbolic-ref --short HEAD`), `IncomingBranch` (parses `MERGE_HEAD`'s
  ref), `ParseConflictRegions`, `ParseInlineConflict` (compat wrapper),
  `ExtractConflictVersions`, `extractStagedVersions`,
  `extractInlineVersions`, `showStage` (`git show :N:path`),
  `MatchConflictRegion`.
- `conflictedFileExtensions` — allow-list of source extensions scanned
  for inline markers during the filesystem walk.

#### Algorithms & Logic
- **`ParseConflictRegions` — a 4-state line machine** (`0` normal, `1`
  ours, `2` base/diff3, `3` theirs) over `strings.Split(content, "\n")`
  with trailing-newline preservation:
  - `<<<<<<<` in state 0 opens a region (recording `OursLabel`); nested
    opens are kept as content and flag `Malformed`.
  - `|||||||` (diff3 base) only valid in state 1; sets `HasBase` +
    `BaseLabel`; outside a region or after `=======` → `Malformed`.
  - `=======` transitions 1→3; outside → malformed stray separator.
  - `>>>>>>>` finishes the region (appends with `EndLine`, index =
    position), state back to 0.
  - EOF with an open region → `finish(..., incomplete=true)`, preserving
    captured content (`Incomplete`).
  - Lines outside regions are appended to *all three* full-file views;
    region lines go to their section — reconstructing `OursText`/
  - `TheirText`/`BaseText` for base-less (non-diff3) files from
    surrounding common lines.
- **`ExtractConflictVersions(ctx, repoDir, filename)`** — cancellation
  check first; `listUnmerged`; if the file has unmerged index entries →
  `extractStagedVersions` which *requires* stages 1/2/3 (any missing →
  `*MissingStageError`) reading each via `showStage`; otherwise (not
  staged, or index unavailable — e.g. fixture dirs) →
  `extractInlineVersions`, which reads the file, parses regions, and
  errors with "no conflict markers found" if there are none. Cancellation
  errors are never converted into the inline fallback.
- **`GetConflictedFiles`** — union of `listUnmerged` (staged) and a
  filesystem walk over `conflictedFileExtensions` parsing inline markers;
  propagates real failures (non-git dir, unreadable file, broken symlink)
  instead of reporting "no conflicts"; output sorted; ctx-aware.
- **`MatchConflictRegion`** — resolves an analysis collision line to its
  region: single region short-circuit → line-range containment → content
  overlap (ours/theirs/base substring) → nearest region by absolute line
  distance.

#### Wiring (Dependencies)
- Imports: stdlib only (`os/exec`, `os`, `path/filepath`, `strings`,
  `bytes`, `context`, `fmt`, `sort`).
- Imported by: `internal/engine` (discovery + version extraction),
  `internal/fileutil` (region-context hashing for apply safety),
  `internal/safeguard`, `internal/patch`, `internal/suggestions`,
  `internal/compare`, `cmd/serve` (branch metadata).

### `backend/internal/git/compare.go`

#### Primary Role
Read-only Git plumbing for the hypothetical-merge comparison: ref
resolution, merge base, commit-tree listing/diffing, blob hashes, file
content at a commit, cross-repository ancestry probing, and rename
detection.

#### Key Structures
- Sentinels `ErrNoMergeBase` ("no common merge base") and
  `ErrFileNotInTree` ("file not present in commit tree") — typed,
  `errors.Is`-matchable conditions that callers turn into first-class
  result states instead of failures.
- `ResolveRef(ctx, repoRoot, ref) (string, error)` —
  `git rev-parse --verify <ref>^{commit}`.
- `FindMergeBase(ctx, repoRoot, ref1, ref2)` — `git merge-base`; exit
  code 1 is mapped to `ErrNoMergeBase`.
- `ShowFileAtCommit(ctx, repoRoot, commitSha, relPath)` — `git show
  <sha>:<path>` (Windows path separators normalized to `/`).
- `ListCommitFiles(ctx, repoRoot, commitSha)` — all paths in a commit
  (used for unrelated-repo full-tree mode).
- `DiffTrees(ctx, repoRoot, commit1, commit2)` — `git diff --name-only`
  → non-empty sorted-ish path list.
- `FileHashAtCommit(ctx, repoRoot, commitSha, relPath)` —
  `git ls-tree <sha> <path>`, returns the blob SHA (3rd whitespace
  field); no entry → wraps `ErrFileNotInTree`.
- `RenameInfo{OldPath, NewPath, Score}` +
  `DetectRenames(ctx, repoRoot, commit1, commit2)` — parses
  `git diff -M --name-status`, keeping `R<score>` rows
  (`fmt.Sscanf(parts[0], "R%d", &score)`).
- `CheckRepositoriesSharedHistory(ctx, repo1, repo2, ref1, ref2)` —
  same-repo: direct `FindMergeBase` (`ErrNoMergeBase` → not shared, not
  an error); cross-repo: bidirectional probes with
  `GIT_ALTERNATE_OBJECT_DIRECTORIES=<other repo's objects>` so ancestry
  can resolve without fetching or mutating either repo; exit code 1 =
  "try the other direction"; both exhausted → `(false, "", nil)`
  (unrelated repositories).
- `getGitObjectsDir` — `git rev-parse --git-path objects` with a
  `.git/objects` fallback; used to build the alternate-object probe.

#### Algorithms & Logic
Every helper uses `exec.CommandContext` (cancellation-aware) with
`cmd.Dir = repoRoot`; failures wrap the underlying error with operation
context; `ctx.Err()` is checked before classifying failures, so
cancellation is never mistaken for "merge base absent". The cross-repo
probe deliberately treats exit code 1 as *data* (no shared ancestor from
this direction) and only hard-fails on other exit codes.

#### Wiring (Dependencies)
- Imports: stdlib (`os/exec`, `errors`, `bytes`, `os`, `path/filepath`…).
- Imported by: `internal/compare` (sole consumer of most helpers);
  `cmd/compare` uses the sentinels for exit-code classification.

## Cross-Module Flow

- `engine.FindConflicts` → `FindGitRepositoryRoots` →
  `GetConflictedFiles` → per file `ExtractConflictVersions` →
  `ConflictData{Regions, Base/Ours/Theirs, Source}` → parser/semantic
  pipeline.
- `cmd/serve` uses `CurrentBranch`/`IncomingBranch` for repository
  metadata shown in the UI.
- `compare.CompareCommits` → `ResolveRef` ×2 → `FindMergeBase` /
  `CheckRepositoriesSharedHistory` → `DiffTrees` + `DetectRenames` →
  `buildLogicalFiles` (rename-aware) → per file `FileHashAtCommit` +
  `ShowFileAtCommit` → AST/semantic analysis — with `ErrNoMergeBase` →
  `UnrelatedRepositories` fallback and `ErrFileNotInTree` → explicit
  per-file classification.
- `fileutil.RegionContextHash` uses `ConflictRegion` so apply-time safety
  checks bind to the exact region shape parsed here.

## Tests

- `backend/internal/git/git_test.go` — `ParseConflictRegions` matrix
  (standard, none, multiple, adjacent, diff3, diff3 empty base, missing
  `>>>>>>>`, stray separator, nested markers); `ExtractConflictVersions`
  (staged modify/modify, missing stage 1 add/add, missing stage 2
  deleted-by-us, inline-only, context cancellation);
  `GetConflictedFiles` sorted output; `FindGitRepositoryRoots`
  cancellation.
- `backend/internal/git/discovery_test.go` — error propagation:
  non-git dir surfaces error, empty repo is *not* an error, broken symlink
  and unreadable file propagate, cancellation respected.
