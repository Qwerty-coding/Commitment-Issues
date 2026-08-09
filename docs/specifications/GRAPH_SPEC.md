# Semantic Explorer Specification

---

| Field | Value |
|--------|--------|
| Document | Graph Specification |
| Project | MergeGraph AI |
| Version | 2.0 |
| Primary Library | Cytoscape.js |

---

# Purpose

The Semantic Explorer is the primary interface through which developers understand merge conflicts.

Unlike conventional graph visualizations that expose every relationship simultaneously, the Semantic Explorer progressively reveals semantic information based on user interaction and conflict relevance.

Its objective is not to display every node.

Its objective is to improve understanding while minimizing cognitive load.

---

# Design Philosophy

The graph should answer questions.

Not display data.

Every interaction should help the user answer one of the following:

- Why is this conflict happening?
- Which functions are involved?
- Which dependencies matter?
- What code will be sent to the AI?
- Why was this code selected?

If an interaction does not improve understanding, it should not exist.

---

# Visualization Goals

The Semantic Explorer should:

- Explain relationships.
- Reduce visual clutter.
- Scale to large repositories.
- Support conflict investigation.
- Support dependency exploration.
- Support prompt understanding.

---

# Core Principle

The graph is **hierarchical**, not flat.

Users should never see thousands of nodes at once.

Information is revealed progressively.

---

# Graph Hierarchy

```
Repository

│

├── Folder

│

├── File

│

├── Function

│

├── Variables

│

├── Function Calls

│

└── Dependencies
```

Each level expands only when requested.

---

# Node Types

## Repository

Represents the entire Git repository.

Children

- folders

---

## Folder

Represents a directory.

Children

- files

---

## File

Represents a source file.

Metadata

- filename
- language
- path

Children

- functions
- classes
- imports

---

## Function

Represents a function or method.

Metadata

- name
- signature
- file
- line range
- token estimate
- conflict status

Children

- variables
- calls
- dependencies

---

## Variable

Metadata

- type
- scope
- read/write count

---

## Import

Metadata

- module
- alias

---

## Conflict

Represents the root of a merge conflict.

Only one conflict node exists per conflict scope.

---

# Edge Types

| Type | Meaning |
|------|----------|
| CALLS | Function invocation |
| READS | Variable read |
| WRITES | Variable write |
| DECLARES | Ownership |
| BELONGS_TO | Hierarchy |
| DEPENDS_ON | Semantic dependency |
| IMPORTS | Import relationship |
| CONFLICTS_WITH | Merge conflict |
| INCLUDED_IN_CONTEXT | Selected for prompt |

Every edge has semantic meaning.

---

# Layout Strategy

Different hierarchy levels use different layouts.

Repository

↓

Breadth-first

Folders

↓

Grid

Files

↓

Grid

Functions

↓

Dagre (top-down call graph)

Dependencies

↓

Concentric

Conflict Scope

↓

Force-directed

This avoids using a single layout for every situation.

---

# Progressive Expansion

Initially the graph shows only

Repository

↓

Folders

↓

Files

No functions are visible.

When a file is expanded

↓

Functions appear.

Variables remain hidden.

When a function is expanded

↓

Variables

↓

Calls

↓

Dependencies

appear.

Only requested information is rendered.

---

# Rendering Rules

Nodes should never appear without context.

If a node is displayed

its parent must already be visible.

Edges should only connect visible nodes.

Collapsed nodes should summarize hidden content.

Example

```
auth.go

▼ 8 Functions
```

instead of displaying all eight.

---

# Interaction Model

Single Click

Select node.

Double Click

Expand or collapse.

Hover

Preview metadata.

Right Click

Context menu.

Search

Focus matching nodes.

---

# Inspector Panel

Selecting a node opens the Inspector.

Repository

- name
- branches
- commits

File

- path
- language
- conflicts

Function

- signature
- dependencies
- callers
- callees
- variables
- token estimate

Conflict

- affected files
- affected functions
- prompt size
- recommendation status

---

# Search

Users should search by

- file name
- function name
- class
- variable
- import

Search never hides nodes.

It focuses them.

---

# Filters

Available filters

- Conflicts only
- Functions
- Variables
- Imports
- Changed nodes
- Prompt context
- Dependency depth
- File type

Multiple filters may be combined.

---

# Focus Mode

Focus Mode isolates one conflict.

Everything unrelated fades.

Only semantic dependencies remain visible.

This is the default mode for large repositories.

---

# Context Mode

Context Mode visualizes exactly what will be sent to the AI.

Included nodes are highlighted.

Excluded nodes become translucent.

This directly supports the project's USP of explainable context optimization.

---

# Graph States

The Semantic Explorer operates in four distinct states.

1 Repository Overview

2 Conflict Investigation

3 Dependency Exploration

4 Prompt Context Review

Each state changes both the layout and the visible information.

---

# Cytoscape Configuration

Recommended extensions

- cytoscape-dagre
- cytoscape-expand-collapse
- cytoscape-popper
- cytoscape-cxtmenu
- cytoscape-node-html-label
- cytoscape-edgehandles (future)

---

# Graph JSON Schema

Each node

```json
{
  "id": "func_login",
  "type": "function",
  "label": "login()",
  "parent": "auth.go",
  "metadata": {
    "file": "auth.go",
    "lines": "42-78",
    "conflict": true,
    "tokens": 118
  }
}
```

Each edge

```json
{
  "source": "login",
  "target": "validateUser",
  "type": "CALLS"
}
```

---

# Performance Strategy

Large repositories should never generate one giant graph.

Instead

- Lazy loading
- Expand on demand
- Viewport culling
- Cached layouts
- Virtual rendering

Only visible nodes consume rendering resources.

---

# Future Enhancements

- Minimap
- Time-travel through Git history
- Animated dependency tracing
- Heatmap overlays
- Dependency impact scoring
- AI explanation overlays
- Multi-user collaboration

---

# Summary

The Semantic Explorer is not a visualization of source code.

It is an interactive explanation of software semantics.

Its purpose is to help developers understand conflicts, dependencies, and AI context through progressive exploration rather than overwhelming visual complexity.

---

**End of Graph Specification**
┌──────────────┬───────────────────────────────┬──────────────────────┐
│ Repository   │      Semantic Explorer        │      Inspector        │
│ Navigator    │      (Cytoscape Graph)        │                      │
│              │                               │ Node Details         │
│ Files        │  Expand / Collapse            │ Dependencies         │
│ Conflicts    │  Focus Mode                   │ AI Context           │
│ Search       │  Context Mode                 │ Token Estimate       │
│ Filters      │                               │ Source Preview       │
└──────────────┴───────────────────────────────┴──────────────────────┘
                     ▲
                     │
             Bottom Drawer (Collapsible)
        Prompt Preview | Logs | Reports | AI Output