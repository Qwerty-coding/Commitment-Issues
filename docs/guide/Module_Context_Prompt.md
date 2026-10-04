# Module: Context & Prompt

## Purpose

This module owns the *prompt-facing* data contract: building the
`PromptContextIR` (what the model is told about the repository, README,
scoped functions and imports), rendering the actual prompt text with
language-correct code fences, and serializing the collision payload in a
compact TOON-style encoding. It also defines `FileAnalysis`, the bundle
the API exposes per analyzed file (including Phase-2 safety hashes).
Storage of these values is deliberately *not* here — it is run-scoped and
lives in `internal/runstate`.

## Files

### `backend/internal/context/prompt_context.go`

#### Primary Role
Constructs `PromptContextIR` for one conflicted file and renders the
text payload sent to the AI model.

#### Key Structures
- `PromptContextIR{RepositorySummary, RepositoryReadme
  (json:"repositoryReadme,omitempty"), Files []string, Functions
  []parser.CodeElement, Variables []parser.CodeElement
  (json:"variables,omitempty"), Imports []string, Context string,
  EstimatedTokens int}` — the JSON contract consumed by the frontend
  (`repositoryReadme` must not be renamed; `Variables` holds
  conflict-scope variable declarations and is trimmed last by the
  budget).
- `BuildPromptContext(repositorySummary, repositoryReadme string,
  files []string, scope semantic.SemanticGraph, ourASTData, theirASTData
  parser.ASTContext) PromptContextIR` — the single entry point.
- `ApplyTargetBudget(ir PromptContextIR, targetTokens int)
  PromptContextIR` — the soft-budget trimmer (see below);
  `DefaultTargetPromptTokens` = 8000 is the package-level default.
- Helpers: `mergeImports`, `EstimateTokens` (`(len(trimmed)+3)/4` —
  ceil of chars/4), `buildCodeElementIndex` (identity-keyed index of
  Functions+Variables), `renderPromptContext`, `uniqueStrings`.

#### Algorithms & Logic
- **Scoping**: index both sides' symbols by `semantic.SymbolIdentity`
  (ours wins on duplicate keys), then keep only scope nodes whose `Kind`
  is `Function` (case-insensitive) and that resolve in the index,
  deduped by identity, **sorted by line → name → kind** for a stable
  prompt order.
- **Imports**: `mergeImports` concatenates both sides' import `Content`,
  trims, dedupes, and sorts — one deterministic list.
- **Rendering order** (`renderPromptContext`): repository summary →
  `Repository README (excerpt):` section (only when non-empty; newline
  normalized) → `Files in scope:` → `Relevant functions:` with
  `### name (Kind, line N)` headers and a **language-aware fence**
  (```` ```<language> ```` derived from the first file via
  `parser.GetLanguageName`; plain ``` when unknown — a wrong fence label
  mis-anchors the model) → `Imports:` → closing instruction line.
- The IR's `Context` is exactly this rendered text; `EstimatedTokens`
  derives from it, and the excerpt therefore counts toward
  `AI_MAX_PROMPT_BYTES` automatically.
- **Soft budget (`ApplyTargetBudget`)**: when the engine's
  `AITargetPromptTokens` is > 0 (env `AI_TARGET_PROMPT_TOKENS`, default
  `DefaultTargetPromptTokens` = 8000; 0 disables), the assembled IR is
  re-rendered until `EstimatedTokens` fits the target. Trim order is
  strict and documented: the README excerpt first, then scoped
  functions from the tail, then variables from the tail. The collision
  payload is not part of the IR — it is supplied separately to the
  resolver — so it can never be trimmed here.

#### Wiring (Dependencies)
- Imports: `internal/parser` (types + language name), `internal/semantic`
  (`SymbolIdentity`, graph types).
- Imported by: `internal/engine` (builds IR per file with the README from
  the run), `internal/runstate` (stores IR in analysis/prompt-context
  maps), `internal/api` (serves `/api/prompt`), `internal/ai` (receives
  the IR in resolution calls).

### `backend/internal/context/readme.go`

#### Primary Role
Discovers and loads the repo-root README excerpt (the AI pre-context
feature).

#### Key Structures
- `readmeCandidates` — deterministic discovery order: `README.md`,
  `readme.md`, `README.markdown`, `README.txt`, `README`; first match
  wins (case-sensitive where the FS is); **no subdirectory READMEs**.
- `LoadReadmeContext(repoRoot string, maxBytes int) (excerpt, source
  string, err error)` — returns `("", "", nil)` when `maxBytes <= 0` or
  no README exists (missing README is not an error).

#### Algorithms & Logic
Reads the candidate file, normalizes CRLF → LF, and truncates at a
**line boundary** so the excerpt never exceeds `maxBytes` yet never
splits a line. Deterministic: same repoRoot + maxBytes → same excerpt.
(The truncation detail lives in the remainder of the function; the
loader, config gates `AI_README_CONTEXT`/`AI_README_MAX_BYTES`, and
`engine.DefaultReadmeMaxBytes = 4096` complete the feature.)

#### Wiring (Dependencies)
- Imports: stdlib only (`bytes`, `os`, `path/filepath`, `strings`).
- Imported by: `internal/engine.ProcessRepository` (loads once per
  repo → `run.RegisterReadme`), tests.

### `backend/internal/context/store.go`

#### Primary Role
Defines `FileAnalysis`, the complete per-file snapshot the API exposes.

#### Key Structures
- `FileAnalysis{Repository, File, RepositorySummary, PromptContext,
  BaseAST, OurAST, TheirAST, SmartDiff}` + Phase-2 safety fields:
  `ContentHash` (SHA-256 of the *working-tree* file at analysis time —
  what will actually be patched), `ConflictRegions []git.ConflictRegion`
  (regions each suggestion must reference), `RegionContextHashes
  map[string]string` (region index → hash of region text + context lines,
  compared on re-read to detect staleness), `ParserVersion` (stale-cache
  detection across parser upgrades), `AnalysisTimestamp` (informational —
  staleness comes from hashes, not time).

#### Wiring (Dependencies)
- Imports: `internal/git` (ConflictRegion), `internal/parser`,
  `internal/semantic`.
- Imported by: `internal/engine` (constructs it), `internal/runstate`
  (stores it), `internal/api` (serves it), `internal/safeguard`/
  `internal/apply` (consume the hashes/regions for safe application).

### `backend/internal/prompt/prompt.go`

#### Primary Role
Serializes the collision payload handed to the AI next to the context
text — spec TOON (Token-Oriented Object Notation) via the shared
`internal/toon` encoder, instead of JSON.

#### Key Structures
- `AIRequestPayload{FileName, BaseCode, OurCode, TheirCode, OurASTData,
  TheirASTData, SmartDiff}`.
- `MarshalAIRequestPayload(payload) ([]byte, error)` →
  `toon.Marshal(payload)` (spec encoder; deterministic).

#### Algorithms & Logic
`toon.Marshal` emits objects as `key: value` lines with 2-space indent
(json tags and `omitempty` honored), uniform struct slices as tabular
`key[N]{f1,f2,…}:` headers + CSV rows, primitive slices as
`key[N]: v1,v2,…`, and non-uniform slices as `- ` items; strings are
emitted plain when safe and JSON-quoted/escaped otherwise; map keys are
sorted so output is deterministic. At the provider layer
(`internal/ai`) `toon` is the default payload format
(`AI_PAYLOAD_FORMAT` / `--payload-format`); choosing `json` falls back
to `json.Marshal`, and the system prompt gains a one-line note telling
the model to read a TOON payload as structured data (only for the toon
format).

#### Wiring (Dependencies)
- Imports: `internal/toon`, `internal/parser`, `internal/semantic`.
- Imported by: `internal/engine` (renders payload into the CLI report and
  `run.SaveReport` audit trail), `internal/api` (prompt endpoint
  consumers via the stored payload).

## Cross-Module Flow

- `engine.ProcessConflictFile` → `BuildPromptContext(summary, readme,
  files, conflictScope, ourAST, theirAST)` → `PromptContextIR` →
  registered on the run → `ai.ResolveCollisionWithRetry(collision,
  promptCtx)` uses `promptCtx.Context` as the model's project context.
- `prompt.MarshalAIRequestPayload(payload)` (spec TOON) →
  `report.PrintPayloadJSON` (CLI) + `run.SaveReport` (audit) — same
  bytes both places; `internal/ai` re-encodes the collision the same
  way when building the model request, or falls back to JSON via
  `AI_PAYLOAD_FORMAT=json`.
- `FileAnalysis` flows engine → runstate → API → frontend; its hashes
  later gate `apply`/`revert` in the mutation stack.

## Tests

- `backend/internal/context/prompt_context_test.go` — scoping/ordering,
  import merging, fence language selection, IR JSON keys (including
  `TestBuildPromptContext_PopulatesRepositoryReadme`), README-section
  rendering, and per-repository README isolation
  (`TestRegisterReadme_MultiRepoIsolation`).
- `backend/internal/context/readme_test.go` — discovery order, CRLF
  normalization, line-boundary truncation at `maxBytes`, missing-README
  tolerance, determinism.
