# Module: Cache

## Purpose

The cache package implements incremental parsing: a bounded, thread-safe
LRU cache of immutable AST results keyed by content identity, with
optional checksummed disk persistence and corruption recovery. It is what
makes repeated analysis (three versions per file, re-runs, comparisons)
cheap — and it is explicitly instance-scoped (per run / per comparison),
never a package global.

## Files

### `backend/internal/cache/cache.go`

#### Primary Role
The `Cache` interface and its sole implementation `ASTCache`: key
construction, LRU memory store with byte-level accounting, atomic
get-or-parse, invalidation, stats, and the optional persistent disk
tier.

#### Key Structures
- Bounds: `DefaultMaxEntries = 10000`, `DefaultMaxMemoryBytes = 50 MB`,
  `DefaultMaxDiskBytes = 100 MB`.
- `Key{RepoID, ContentHash, RelPath, Language, ParserVersion,
  SchemaVersion}` — identity of a parse result;
  `HashKey()` is a SHA-256 over normalized fields (path via
  `filepath.ToSlash(filepath.Clean(...))` for cross-platform stability);
  `String()` gives a compact human-readable form.
- `Entry{Key, AST parser.ASTContext, ContentHash, CachedAt, SizeBytes}`
  — immutable; `Clone()` deep-copies via `ASTContext.Clone()` so callers
  can never mutate cached state. Secrets/prompts/run config are never
  stored.
- `Stats{Hits, Misses, Stores, Evictions, Corruptions}` (atomic counters)
  + `HitRatio()`.
- `Config{MaxEntries, MaxMemoryBytes, EnableDisk, DiskDir, MaxDiskBytes,
  ParserVersion, SchemaVersion}` + `DefaultConfig()` (disk **disabled**
  by default; versions pinned to `parser.CurrentParserVersion` /
  `CurrentSchemaVersion`).
- `Cache` interface: `Get`, `Put`, `GetOrParse(ctx, repoID, relPath,
  source)`, `Invalidate(predicate)`, `InvalidateFile(repoID, relPath)`,
  `Clear`, `Stats`, `ResetStats`, `Close`.
- `ASTCache` — `sync.RWMutex` over `map[string]*list.Element` +
  `container/list` LRU, `curMemory` byte accounting, atomic stat
  counters, and `fileLocks sync.Map` (per-key mutexes serializing disk
  read/write of one entry).
- Constructors `New(cfg)` (normalizes zero values, `MkdirAll` for the
  disk dir) and `NewDefault()`.

#### Algorithms & Logic
- **Lookup order**: memory first (version re-verification on hit —
  parser/schema mismatch evicts the stale element), then disk (if
  enabled), then miss. Hits return `entry.Clone()`.
- **`GetOrParse`**: cancellation checked before parse and again after —
  the guarantee is that a cancelled parse leaves **no partial entry**;
  on success (or on the deliberate "soft" errors `ErrFileTooLarge` /
  `ErrBinaryFile`, whose diagnostic-bearing contexts are worth caching)
  the entry is published atomically via `Put`.
- **LRU eviction** (`putMemoryLocked`): while entries exceed
  `MaxEntries`, or memory exceeds `MaxMemoryBytes` (keeping at least one
  entry), evict from the back; `curMemory` adjusted (clamped ≥ 0).
- **Disk tier**: `diskPayload{Entry, Checksum}` JSON per key file;
  writes are temp-file + `Remove` + `Rename` (atomic on Windows and
  Unix); reads validate JSON, entry presence, the SHA-256 checksum
  (`computePayloadChecksum` over key fields + symbol counts), and
  parser/schema versions — any failure increments `Corruptions` and
  **deletes** the bad file (self-healing). Per-key file locks prevent
  interleaved read/write.
- **Disk budget** (`enforceDiskLimits`): after each write, if total
  `.json` bytes exceed `MaxDiskBytes`, evict oldest-modified files first
  down to 80% of the budget.
- **`estimateEntrySize`**: sums key strings plus per-element
  name/kind/content/scope/signature lengths (+ fixed overheads) to drive
  memory accounting without serializing the AST.

#### Wiring (Dependencies)
- Imports: `CommitIssues/internal/parser` (ParseAndExtract, HashSource,
  versions), stdlib (`container/list`, `crypto/sha256`, `encoding/json`,
  `sync`, `sync/atomic`, `os`, `path/filepath`, `sort`, `time`).
- Imported by: `internal/engine` (per-run cache injection; `Config.ASTCache`),
  `internal/compare` (per-comparison cache via `Options.Cache`), and
  constructed in `cmd` configs (`engine.DefaultConfig`).

## Cross-Module Flow

- `engine.ProcessConflictFile` → `astCache.GetOrParse(ctx, repoRoot,
  file, versionBytes)` ×3 (base/ours/theirs) → `parser.ParseAndExtract`
  on miss → cloned `parser.ASTContext` → semantic diff.
- `compare.CompareCommits` → `Options.Cache.GetOrParse` for the two
  commit versions of each file.
- Cache identity includes `ParserVersion`/`SchemaVersion`, so bumping
  `parser.CurrentParserVersion` invalidates everything by construction.

## Tests

- `backend/internal/cache/cache_test.go` — key determinism, hit/miss and
  clone isolation, LRU + memory eviction, `GetOrParse` atomicity under
  cancellation, invalidation (`Invalidate`/`InvalidateFile`), disk
  persistence round-trip, checksum corruption recovery, and stats.
