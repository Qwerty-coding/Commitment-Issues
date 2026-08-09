# MergeGraph AI — Memory

> This is the living memory of the project.
> It records the current state, important decisions, active work, and future direction.
> Update this file after every significant development session.

---

# Project Snapshot

Project: MergeGraph AI

Status: Active Development

Current Version: v0.2.0

Current Phase:
Semantic Analysis Engine → transitioning into Context Optimization / API surfacing

Current Milestone:
Prompt Context Generation (Engine 6) — core logic complete, REST surface not yet exposed

---

# Project USP

Extract the **smallest semantically complete context**
required to understand a merge conflict before sending
anything to an LLM.

Primary goal:
Reduce LLM token usage while maintaining semantic correctness.

---

# Tech Stack

Backend
- Go

Frontend
- React
- TailwindCSS
- Cytoscape.js

Parser
- Tree-sitter

Git
- Git CLI

AI
- Gemini (Pluggable)

---

# Current Progress

## ✅ Completed

- Repository analysis
- Git integration
- Branch detection
- Merge conflict detection
- Package migration to `internal/*` layout (git, parser, semantic, graph, api, ai, context, report) — matches Architecture Rule 1 (Layer Boundaries) and Rule 2 (Single Responsibility)
- Semantic Graph Builder — `BuildSemanticGraph` extracts `CALLS` edges from Tree-sitter `call_expression` nodes, reusing the existing AST walk
- Symmetric branch merge — `MergeSemanticGraphs` unions ours/theirs semantic graphs so conflict scope isn't blind to either side (base still feeds `SmartDiff` classification only)
- Conflict Scope Extraction — `ComputeConflictScope` does BFS from colliding functions outward via CALLS edges (both directions), correctly terminates when no new nodes are reached
- Graph output is scope-driven — `BuildCyGraph` now receives the computed `conflictScope`, not the full merged graph; edges only added when both endpoints resolve to real Cytoscape node IDs (no dangling edges)
- `/api/graph` response wrapped in the documented `{success, message, data}` envelope
- Prompt Context Generation — `internal/context.BuildPromptContext` filters the conflict scope down to just the relevant function source, renders a structured prompt, estimates tokens
- AI call now sends the **scoped** prompt context to Gemini instead of full file sources; full-payload benchmark still runs alongside it so token reduction is directly comparable and printed per collision
- Removed all duplicate/dead code left over from the package migration (old Gemini transport + report-printing forks in `main.go`, duplicated request-building between `resolveCollisionWithAI` and `runFullPayloadBenchmark` in `ai.go`, now unified through a shared `callGemini` helper)

## 🚧 In Progress

- None actively — last completed unit (AI-layer dedup) is closed out

## ⏳ Next

- `/api/prompt` and `/api/prompt/statistics` REST endpoints (data already exists via `PromptContextIR`, mostly wiring — same shape as the `/api/graph` work)
- Import extraction (`FR-17`) — `PromptContextIR.Imports` is currently always empty; parser doesn't extract imports yet
- Automated tests for `internal/semantic` (conflict scope traversal is the most algorithmically deep piece in the codebase and currently has zero coverage — required per Development Rules' Testing Rules)
- Cytoscape Integration (frontend still needs to consume the now-real edges)

---

# Current Focus

Everything currently revolves around one question:

How can we determine the minimum semantic context required
to explain a merge conflict?

Do not work on unrelated UI features until this pipeline is complete.

**Status update:** this pipeline is now functionally complete end-to-end — Git extraction → AST parsing → semantic graph → conflict scope → scoped prompt context → AI call. What remains is exposing it over REST (`/api/prompt/statistics`) and adding test coverage, not new core logic.

---

# Important Decisions

## Semantic Graph is the source of truth.

Everything uses it.

- Visualization
- Prompt Builder
- Conflict Scope
- Reports

Never duplicate dependency logic elsewhere.

## Static Analysis before AI

AI never decides context.

Backend decides context.

AI only explains.

**Confirmed in implementation:** `ComputeConflictScope` runs entirely before any Gemini call; the AI receives `PromptContextIR.Context`, a backend-assembled string, never raw scope data it could reinterpret.

## Progressive Graph

Never render the entire repository.

Repository

↓

Folders

↓

Files

↓

Functions

↓

Variables

Load everything lazily.

## Manual Approval

AI never edits repositories automatically.

Developer always approves.

## Symmetric branch merge for conflict scope (new)

Conflict scope is computed from the **union** of ours' and theirs' semantic graphs, not from ours alone. Node identity is name+kind based, so a same-named function collapses to one node across branches (correct — it's the same semantic entity being reconciled), while edges from both branches survive independently. `base` still only feeds the 3-way `SmartDiff` classification (ADDED/UPDATED/DELETED/COLLISION), not the scope graph itself.

Known cosmetic side-effect: `SemanticNode.Calls` (the string-slice field on the node, separate from `Edges`) keeps whichever side's node was seen first during merge. Not currently read anywhere downstream — only `Edges` drives scope traversal and Cytoscape output — but flagged so a future feature reading `node.Calls` directly doesn't get bitten by stale data.

---

# MVP Scope

Included

✓ Repository Analysis

✓ Tree-sitter Parsing

✓ Semantic Graph

✓ Conflict Scope

✓ Prompt Builder

✓ Cytoscape Explorer

✓ AI Recommendation

Excluded

✗ Context Scoring Engine

✗ ML Ranking

✗ Local LLM

✗ IDE Plugin

✗ GitHub App

These belong after MVP.

---

# Known Issues

- `PromptContextIR.Imports` always empty — import extraction not yet implemented in `internal/parser`
- `/api/prompt` and `/api/prompt/statistics` endpoints not implemented — `PromptContextIR` exists but has no REST surface yet
- No automated tests anywhere in the backend (violates Development Rules' Testing Rules, which require coverage for Parser, Semantic Extraction, Conflict Scope, Prompt Generation, API endpoints)
- `SemanticNode.Calls` reflects only the first-seen branch after `MergeSemanticGraphs` (cosmetic, unused downstream — see Important Decisions)

---

# Current TODO

High Priority

- `/api/prompt/statistics` endpoint
- Test coverage for `internal/semantic` (conflict scope BFS especially)
- Cytoscape Integration (consume real edges on the frontend)

Medium Priority

- Import extraction (`FR-17`)
- Prompt Preview (frontend)
- Inspector Panel

Low Priority

- Export Reports
- UI Animations

---

# Files Being Modified

Backend

internal/git/
internal/parser/
internal/semantic/
internal/context/
internal/ai/
internal/graph/
internal/api/
internal/report/

Frontend

components/

pages/

graph/

---

# Documentation

Completed

✓ PRD

✓ Architecture

✓ Core Engine

✓ Graph Spec

✓ API Spec

✓ Semantic Analysis

✓ Design

✓ Roadmap

✓ Development Rules

Remaining

None

---

# Rules for Future AI

Before changing code:

1. Read PRD.
2. Read Architecture.
3. Read Development Rules.
4. Read this Memory file.

Never

- Change architecture without reason.
- Add post-MVP features.
- Increase token usage unnecessarily.
- Break API contracts.

Always

- Keep changes modular.
- Explain important decisions.
- Update documentation if architecture changes.

---

# Session Log

## 2026-08-05

Completed

- Migrated monolithic `main.go`/`graph_adapter.go` into `internal/{git,parser,semantic,graph,api,ai,context,report}` packages
- Implemented `CALLS` edge detection in `internal/parser` (reusing existing Tree-sitter walk)
- Implemented real `SemanticGraph` (nodes + typed edges) in `internal/semantic`
- Implemented `ComputeConflictScope` (Algorithm 4 — BFS from colliding functions, both edge directions, terminates correctly)
- Fixed conflict scope to merge ours+theirs semantic graphs symmetrically instead of ours-only
- Wired scope graph into `BuildCyGraph` so Cytoscape output is scope-driven, not full-graph
- Implemented `PromptContextIR` + `BuildPromptContext` in `internal/context`
- Wired scoped prompt context into the actual Gemini call (previously sent full file sources)
- Removed dead/duplicate code: old AI transport fork in `main.go`, old report-printing fork in `main.go`, duplicated Gemini request-building between `resolveCollisionWithAI` and `runFullPayloadBenchmark` (unified via `callGemini`)
- Confirmed `/api/graph` envelope matches `API_SPEC.md`

Next

- `/api/prompt/statistics` endpoint
- Test coverage for `internal/semantic`
- Import extraction

Problems

- None blocking; all known gaps are tracked above under Known Issues

Notes

- Every step in this session kept the project compiling; no big-bang rewrites
- All changes preserved existing algorithms — this was structural/wiring work, not logic redesign

---

# Future Ideas

- Context Scoring Engine
- Multi-language analysis
- Graph heatmaps
- IDE integration
- Repository embeddings

These are ideas, not active tasks.

---

# Guiding Question

Every feature should answer:

"Does this help identify the smallest semantically complete context for a merge conflict?"

If not, reconsider whether it belongs in the project.