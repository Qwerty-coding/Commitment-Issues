# System Architecture

---

| Field | Value |
|-------|-------|
| **Document** | Architecture Specification |
| **Project** | MergeGraph AI |
| **Version** | 2.0 |
| **Status** | Draft |
| **Related Documents** | PRD.md, CORE_ENGINE.md, GRAPH_SPEC.md, API_SPEC.md |

---

# Purpose

This document defines the complete software architecture of MergeGraph AI.

It describes:

- overall system architecture
- processing pipeline
- module responsibilities
- data flow
- frontend/backend interaction
- graph generation
- AI integration
- scalability strategy
- future extensibility

This document is the primary technical blueprint for implementing the system.

---

# Design Philosophy

MergeGraph AI is built around one central idea:

> **Everything revolves around semantic understanding.**

Unlike traditional merge tools that compare files, MergeGraph AI analyzes software structure.

The system first understands code.

Only then does it visualize it.

Only then does it ask AI to reason about it.

Every architectural decision follows four principles:

1. Static analysis before AI.
2. Semantic understanding before visualization.
3. Explainability before automation.
4. Minimal context before prompt generation.

---

# High-Level System Architecture

```
                    Git Repository
                           │
                           ▼
                Repository Analysis Engine
                           │
                           ▼
                  Merge Conflict Detector
                           │
                           ▼
                   Git History Analyzer
                           │
                           ▼
                  Tree-sitter Parser
                           │
                           ▼
                  Abstract Syntax Tree
                           │
                           ▼
                Semantic Analysis Engine
                           │
                           ▼
                 Semantic Graph Builder
                           │
            ┌──────────────┼──────────────┐
            ▼              ▼              ▼
    Visualization     Conflict Scope    Reports
        Engine            Engine         Engine
                           │
                           ▼
                Context Optimization
                           │
                           ▼
                   Prompt Builder
                           │
                           ▼
                      AI Provider
                           │
                           ▼
                 Merge Recommendation
                           │
                           ▼
                  Developer Approval
                           │
                           ▼
                   Patch Generation
```

---

# Core Architectural Principle

The architecture is centered around a single data structure.

```
Semantic Graph
```

Every major module either produces it,
consumes it,
or enriches it.

The Semantic Graph is therefore the source of truth for the entire system.

---

# System Layers

The platform consists of seven architectural layers.

```
Presentation Layer

↓

Application Layer

↓

Analysis Layer

↓

Semantic Layer

↓

Optimization Layer

↓

AI Layer

↓

Output Layer
```

Each layer has one responsibility.

---

# Layer 1 — Presentation Layer

Purpose

Provide an interactive interface for developers.

Technologies

- React
- Tailwind CSS
- Cytoscape.js

Responsibilities

- Repository selection
- Conflict selection
- Graph interaction
- Inspector panel
- Search
- Filters
- Prompt preview
- Export

Consumes

- Graph JSON
- Reports
- Prompt Context

Produces

User interactions.

---

# Layer 2 — Repository Analysis Layer

Purpose

Understand repository structure.

Responsibilities

- Repository loading
- Branch discovery
- Commit history
- Merge detection
- Conflict detection

Input

Git Repository

Output

Repository Metadata

Conflict Metadata

Commit Metadata

---

# Layer 3 — Parsing Layer

Purpose

Convert source code into structured syntax trees.

Technology

Tree-sitter

Responsibilities

- Parse files
- Generate AST
- Detect syntax errors
- Preserve source locations

Input

Source Files

Output

AST

---

# Layer 4 — Semantic Analysis Layer

Purpose

Transform syntax into meaning.

Responsibilities

Extract

- Functions
- Methods
- Classes
- Structs
- Variables
- Imports
- Exports
- References

Discover

- Calls
- Reads
- Writes
- Dependencies

Output

Semantic Graph

This is the heart of the system.

---

# Layer 5 — Context Optimization Layer

Purpose

Determine the minimum semantically complete context.

Responsibilities

Locate

Conflict Root

↓

Expand

Dependencies

↓

Remove

Irrelevant Nodes

↓

Estimate

Prompt Size

↓

Produce

Prompt Context

Output

PromptContext

---

# Layer 6 — AI Layer

Purpose

Generate merge recommendations.

Responsibilities

- Prompt generation
- AI communication
- Recommendation generation
- Explanation generation

Important

The AI never receives the complete repository.

Only PromptContext.

---

# Layer 7 — Output Layer

Produces

Interactive Graph

↓

Reports

↓

Statistics

↓

Merge Recommendation

↓

Patch

---

# Complete Processing Pipeline

```
Repository

↓

Conflict Detection

↓

Git Analysis

↓

Tree-sitter Parsing

↓

AST

↓

Semantic Extraction

↓

Semantic Graph

↓

Conflict Scope

↓

Dependency Expansion

↓

Context Optimization

↓

Prompt Builder

↓

LLM

↓

Recommendation

↓

Developer Review

↓

Patch
```

---

# Architectural Engines

Rather than organizing the system as backend/frontend,
MergeGraph AI is divided into independent engines.

---

## Repository Engine

Purpose

Repository inspection.

Produces

Repository IR

Consumes

Git

---

## Parser Engine

Purpose

Generate AST.

Produces

AST IR

Consumes

Source Files

---

## Semantic Engine

Purpose

Generate Semantic Graph.

Produces

Semantic Graph IR

Consumes

AST

---

## Conflict Engine

Purpose

Compute semantic conflict scope.

Produces

Conflict Scope IR

Consumes

Semantic Graph

---

## Context Engine

Purpose

Optimize prompt context.

Produces

Prompt Context IR

Consumes

Conflict Scope

---

## Visualization Engine

Purpose

Generate Cytoscape graph.

Produces

Graph JSON

Consumes

Semantic Graph

---

## AI Engine

Purpose

Generate merge recommendation.

Produces

Recommendation

Consumes

Prompt Context

---

## Report Engine

Purpose

Export analysis.

Produces

Reports

Consumes

Every previous stage.

---

# Intermediate Representations (IR)

Every stage produces an immutable intermediate representation.

| Stage | Output |
|--------|---------|
| Repository Analysis | RepositoryIR |
| Parsing | ASTIR |
| Semantic Analysis | SemanticGraphIR |
| Conflict Analysis | ConflictScopeIR |
| Context Optimization | PromptContextIR |
| AI | RecommendationIR |

Advantages

- Easier testing
- Caching
- Language independence
- Modular architecture
- Easier debugging

---

# Backend Architecture

```
cmd/

↓

internal/

├── git/
├── parser/
├── semantic/
├── graph/
├── conflict/
├── context/
├── prompt/
├── ai/
├── report/
└── api/
```

Each package owns one responsibility.

No package should directly manipulate another package's internal data.

---

# Frontend Architecture

```
App

│

├── Sidebar
├── Toolbar
├── Graph Canvas
├── Inspector
├── Prompt Panel
├── Statistics
└── Settings
```

Every panel communicates through a shared application state.

---

# Data Flow

```
Git

↓

Backend

↓

Semantic Graph

↓

REST API

↓

Frontend

↓

Cytoscape

↓

User

↓

AI Request

↓

Backend

↓

Prompt Builder

↓

LLM

↓

Recommendation
```

---

# Communication Architecture

```
Frontend

↓

REST

↓

Backend

↓

Internal Services

↓

Git

Tree-sitter

AI
```

The frontend never communicates directly with Git or the LLM.

---

# Error Handling Strategy

Every stage is isolated.

If one stage fails:

Repository Analysis

↓

Continue

↓

Parser

↓

Continue

↓

Semantic Analysis

↓

Continue

↓

Visualization

↓

Continue

AI failures must never prevent visualization.

Visualization failures must never prevent report generation.

Graceful degradation is preferred over complete failure.

---

# Scalability Strategy

Large repositories are handled using:

- Lazy graph loading
- Expand-on-demand
- Hierarchical visualization
- Incremental graph generation
- Context-focused rendering

The system should never attempt to display the entire repository simultaneously.

---

# Security Considerations

The system performs read-only repository analysis.

Source code is never modified without explicit user approval.

AI requests contain only optimized semantic context.

No repository is transmitted in full.

No merge is applied automatically.

---

# Technology Decisions

| Component | Technology | Reason |
|-----------|------------|--------|
| Backend | Go | Performance, concurrency |
| Parser | Tree-sitter | Accurate AST generation |
| Graph | Cytoscape.js | Large graph visualization |
| Frontend | React | Component architecture |
| Styling | Tailwind CSS | Rapid UI development |
| AI | Gemini/OpenAI | Merge reasoning |
| Version Control | Git CLI | Native Git support |

---

# Future Architecture

Future versions may introduce:

- Multi-language parsing
- Graph database storage
- Incremental semantic indexing
- Local LLM execution
- IDE plugins
- Background repository indexing
- Semantic caching
- Live collaboration
- Distributed analysis

The current modular architecture is intentionally designed so these capabilities can be added without redesigning the existing pipeline.

---

# Architecture Summary

MergeGraph AI is built as a pipeline of independent semantic analysis engines connected through immutable intermediate representations.

The Semantic Graph serves as the central source of truth, enabling visualization, context optimization, reporting, and AI-assisted merge recommendations while maintaining explainability, modularity, and efficient token usage.

---

**End of Architecture Specification**       