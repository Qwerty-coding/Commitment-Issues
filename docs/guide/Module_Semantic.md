# Module: Semantic

## Purpose

The semantic package turns three `ASTContext`s (base/ours/theirs) into the
system's core insight: which *symbols* collided (changed on both sides),
which changed on only one side, plus the call-graph and conflict-scope
structures that drive the UI graph and prompt scoping. Determinism is a
first-class property here — every collection is explicitly sorted so map
iteration order can never leak into output.

## Files

### `backend/internal/semantic/semantic.go`

#### Primary Role
Symbol-level diffing (`GenerateSmartDiff`), semantic call-graph
construction/merging (`BuildSemanticGraph`, `MergeSemanticGraphs`), and
conflict-scope extraction (`ComputeConflictScope`).

#### Key Structures
- `DiffItem{Type, Kind, Name, Line, BaseContent, OurContent,
  TheirContent, File, Identity}` — one classified symbol change;
  `Identity` is the precise symbol key.
- `SmartDiffResult{Collisions, OurChanges, TheirChanges []DiffItem}` —
  the three-way classification output.
- `SemanticNode{ID, Name, Kind, Label, Line, Calls, Identity}`,
  `SemanticEdge{ID, Source, Target, Type}`, `SemanticGraph{Nodes, Edges}`,
  `ConflictScope = SemanticGraph` (type alias) — graph model shared with
  `internal/graph` and the frontend.
- Identity helpers: `SymbolIdentity(parser.CodeElement)` →
  `file|scope|kind|name|signature`, degrading to legacy `kind:name` only
  when file/scope/signature are all empty; `DiffKey(item)` → identity or
  lowercased `kind:name`; `BuildSignatureMap(ctx)` → identity-keyed map of
  Functions+Variables.
- Classification helpers: `SideStatus(baseEl, sideEl, inBase, inSide)` →
  `""` | `ADDED` | `DELETED` | `UPDATED` (UPDATED requires differing
  `Content`); `BuildDiffItem(status, baseEl, sideEl, isOurs)` (DELETED
  items take metadata from the base element; otherwise from the side).
- Ordering: `CompareDiffItems` defines the canonical order
  `identity → file → kind → name → line → type`; `SortDiffItems` applies
  it with `sort.SliceStable`.
- Graph builders: `BuildSemanticGraph`, `MergeSemanticGraphs`,
  `ComputeConflictScope`, plus internal `semanticNodeKey` (lowercased
  `kind:name`), `semanticNodeID` (kind + sanitized name, path/colon/dot
  separators → `_`), and `semanticNodeIDFor` (composite ID from
  file/scope/name/signature, legacy ID only when richer metadata is
  absent).
- Entry points: `GenerateSmartDiffContext(ctx, …)` (cancellation-checked
  wrapper) and `GenerateSmartDiff(ctx, base, ours, theirs)`.

#### Algorithms & Logic
- **SmartDiff**: build identity maps for all three sides; union the
  identities of ours+theirs (cancellation checked inside the loop); for
  each identity compute per-side status against base. A **collision** is
  when *both* sides are `ADDED`/`UPDATED` **and** their contents differ;
  otherwise each non-empty status is emitted into its side's change list.
  All three lists are then sorted with `SortDiffItems`, making output
  independent of Go map ordering.
- **BuildSemanticGraph**: one node per function/variable keyed by
  `semanticNodeIDFor` (so overloads/scoped methods stay distinct), plus
  `CALLS` edges from each Function's `Calls` to the *lowest-ID* function
  node with that lowercased name (`funcByName` tie-break keeps it
  deterministic); edges deduped via `edgeSeen`; final stable sort by ID.
- **MergeSemanticGraphs**: first-wins dedupe of nodes and edges by ID
  across graphs, then stable sort by ID.
- **ComputeConflictScope (BFS)**: seeds from every collision — using the
  collision's *precise* identity when present (never falling back to
  `kind:name`, which could match an unrelated same-named symbol; legacy
  fallback only for identity-less collisions) — then breadth-first
  traverses outgoing and incoming `CALLS` edges, collecting visited nodes
  and traversed edges, finally sorted by ID. Seeds are pre-sorted with
  `CompareDiffItems` so traversal order is deterministic too.

#### Wiring (Dependencies)
- Imports: `CommitIssues/internal/parser` (ASTContext/CodeElement types),
  stdlib (`context`, `fmt`, `sort`, `strings`).
- Imported by: `internal/ai` (providers use DiffItem/scope in prompts),
  `internal/suggestions`, `internal/prompt`, `internal/report`,
  `internal/graph`, `internal/runstate` (stores semantic results),
  `internal/compare`, `internal/context` (scoping of prompt functions),
  `internal/engine` (per-file pipeline).

## Cross-Module Flow

- `engine.ProcessConflictFile` → `GenerateSmartDiffContext` (classify)
  → `BuildSemanticGraph(ours)` + `BuildSemanticGraph(theirs)` →
  `MergeSemanticGraphs` → `ComputeConflictScope` (which collisions
  matter) → `promptcontext.BuildPromptContext` (scoped functions into the
  prompt) and `graph.BuildCyGraph` (UI payload).
- `internal/compare` reuses `GenerateSmartDiff` on the two commit
  versions of a file to label `structural_collision` vs
  `content_conflict`.
- `DiffItem.Identity` is the join key all the way through: prompt
  scoping, suggestion identity, and API responses.

## Tests

- `backend/internal/semantic/semantic_test.go` — classification
  correctness (added/updated/deleted/collision cases) and graph building.
- `backend/internal/semantic/determinism_test.go` — repeated runs produce
  byte-identical ordering (guards against map-iteration leaks).
- `backend/internal/semantic/scope_kinds_test.go` — conflict scope
  seeding across symbol kinds.
- `backend/internal/semantic/semantic_identity_test.go` —
  `SymbolIdentity`/node-ID behavior for overloads, scoped methods, and
  legacy degradation.
