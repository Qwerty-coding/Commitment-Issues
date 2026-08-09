# Core Engine Specification

---

| Field | Value |
|--------|--------|
| Document | Core Engine |
| Version | 2.0 |
| Status | Draft |
| Related Documents | PRD.md, ARCHITECTURE.md, GRAPH_SPEC.md |

---

# Purpose

The Core Engine is responsible for transforming a Git repository into semantically meaningful information that can be visualized, analyzed, and supplied to an AI model.

Unlike conventional merge tools that operate on textual differences, the Core Engine reasons about software structure and dependency relationships.

Every downstream component—including visualization, reporting, and AI—depends on the outputs of this engine.

---

# Design Philosophy

The Core Engine follows five principles.

1. Parse once.

2. Reuse everywhere.

3. Build semantics before visualization.

4. Optimize context before AI.

5. Preserve explainability throughout the pipeline.

Every stage should produce deterministic outputs that can be independently tested and reused.

---

# Complete Processing Pipeline

```
Git Repository

↓

Repository Analysis

↓

Conflict Detection

↓

Tree-sitter Parsing

↓

Abstract Syntax Tree

↓

Semantic Extraction

↓

Semantic Graph

↓

Conflict Scope Analysis

↓

Dependency Expansion

↓

Context Optimization

↓

Prompt Builder

↓

AI Recommendation
```

---

# Internal Data Flow

```
RepositoryIR

↓

ASTIR

↓

SemanticGraphIR

↓

ConflictScopeIR

↓

PromptContextIR

↓

RecommendationIR
```

Each representation is immutable and forms the input to the next stage.

---

# Engine 1 — Repository Analysis

## Purpose

Analyze the Git repository and collect metadata required for downstream processing.

---

### Responsibilities

- Detect repository root
- Validate Git repository
- Discover branches
- Identify merge commits
- Detect conflicts
- Read Git history

---

### Input

Repository Path

---

### Output

RepositoryIR

Contains

- repository metadata
- branches
- commit history
- merge commits
- conflicting files

---

### Complexity

O(number of commits)

---

# Engine 2 — Parser Engine

Purpose

Transform source code into Abstract Syntax Trees.

Technology

Tree-sitter

Responsibilities

- Parse every conflicting file
- Preserve source positions
- Detect syntax errors
- Support incremental parsing

Input

Source Files

Output

ASTIR

---

# Engine 3 — Semantic Extraction

Purpose

Convert syntax into software meaning.

Extract

Functions

Methods

Classes

Variables

Structs

Interfaces

Imports

Exports

Function Calls

Variable Reads

Variable Writes

Dependencies

References

Produces

SemanticGraphIR

This becomes the primary data structure for the remainder of the system.

---

# Engine 4 — Semantic Graph Builder

Purpose

Build a graph representing relationships between software entities.

Node Types

Repository

Folder

File

Function

Method

Variable

Import

Class

Conflict

Prompt Context

Edge Types

CALLS

READS

WRITES

DECLARES

BELONGS_TO

DEPENDS_ON

IMPORTS

EXPORTS

---

Graph Invariants

Every node has a unique identifier.

Every edge has a semantic meaning.

Every relationship is directional.

Every node stores source location.

Every node stores originating file.

---

# Engine 5 — Conflict Scope Engine

Purpose

Determine the smallest semantic region affected by a merge conflict.

Input

SemanticGraphIR

Conflict Metadata

Algorithm

Locate conflict root

↓

Expand semantic dependencies

↓

Follow function calls

↓

Follow shared variables

↓

Include required imports

↓

Stop at semantic boundary

Output

ConflictScopeIR
Conflict Scope

Unlike Git,
which identifies conflicting lines,

the Conflict Scope Engine identifies conflicting software entities.

This distinction is fundamental to MergeGraph AI.

# Engine 6 — Context Optimization Engine

Purpose

Reduce LLM token usage without losing semantic completeness.

Pipeline

Conflict Scope

↓

Rank Dependencies

↓

Remove Irrelevant Nodes

↓

Merge Duplicates

↓

Estimate Tokens

↓

Generate Prompt Context

Optimization Rules

Include

directly modified functions
shared variables
required imports
transitive dependencies

Exclude

unrelated utilities
unused variables
disconnected nodes
dead code

Output

PromptContextIR

# Engine 7 — Prompt Builder

Purpose

Convert PromptContextIR into a structured prompt for an LLM.

Prompt Sections

1 Repository Summary

2 Conflict Summary

3 Relevant Files

4 Relevant Functions

5 Dependencies

6 Required Context

7 Merge Task

The Prompt Builder must never include unrelated code.

# Engine 8 — AI Engine

Purpose

Generate merge recommendations using optimized context.

Responsibilities

Build API request
Send prompt
Receive response
Validate response
Generate explanation

The AI Engine is intentionally stateless.

It never stores repository information.

Complexity Analysis
Stage	Complexity
Git Analysis	O(C)
Parsing	O(F)
Semantic Extraction	O(N)
Graph Construction	O(N + E)
Conflict Scope	O(V + E)
Context Optimization	O(V log V)
Prompt Generation	O(P)

Where

C = commits

F = files

N = AST nodes

V = graph nodes

E = graph edges

P = prompt size

Performance Strategy

The engine is designed around three principles.

Incremental Analysis

Only changed files are reparsed.

Lazy Expansion

Only required graph regions are expanded.

Context Reuse

Previously computed semantic information is reused whenever possible.

Future Enhancements

Future versions may introduce:

Incremental semantic indexing
Graph database persistence
Multi-language repositories
Dependency ranking using ML
Embedding-assisted retrieval
Local LLM support
Graph Neural Networks
Core Engine Summary

The Core Engine transforms raw Git repositories into structured semantic knowledge.

Its primary responsibility is not visualization.

Its primary responsibility is not AI.

Its responsibility is to determine the minimum semantically complete context required for accurate merge conflict reasoning.

Everything else in MergeGraph AI is built on top of this capability.

### Container Docker pull MySQL so by defaultEnd of Core Engine Specification