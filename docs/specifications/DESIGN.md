# Design Specification

---

| Field | Value |
|--------|--------|
| Document | Design Specification |
| Project | MergeGraph AI |
| Version | 2.0 |

---

# Purpose

This document defines the visual language, user experience, interaction patterns, and interface design principles for MergeGraph AI.

The goal is to create a developer tool that is clean, scalable, and easy to navigate, even when analyzing large repositories.

The interface should prioritize clarity, progressive disclosure, and semantic understanding over visual complexity.

---

# Design Philosophy

MergeGraph AI is not a dashboard.

It is not a graph viewer.

It is an investigation tool.

Every screen should help developers answer one question:

> "Why is this merge conflict happening?"

Every UI element must support that objective.

---

# Core Design Principles

The interface follows six principles.

## 1. Progressive Disclosure

Never display all information simultaneously.

Show only what the developer needs.

Reveal more information through interaction.

---

## 2. Context First

Always present context before details.

Repository

↓

Conflict

↓

Function

↓

Variable

---

## 3. Minimal Visual Noise

Avoid unnecessary borders, colors, shadows, and icons.

Whitespace is preferred over decoration.

---

## 4. Explainability

Every highlighted node should have an explanation.

Every AI recommendation should show why it was generated.

---

## 5. Consistency

Every panel behaves consistently.

The same interactions should produce the same results everywhere.

---

## 6. Performance

Large repositories should feel responsive.

The interface should never freeze because of graph size.

---

# Application Layout

```

┌─────────────────────────────────────────────────────────────┐
│ Header                                                      │
├───────────────┬────────────────────────────┬────────────────┤
│               │                            │                │
│ Repository    │     Semantic Explorer      │   Inspector    │
│ Navigator     │                            │                │
│               │                            │                │
│               │                            │                │
├───────────────┴────────────────────────────┴────────────────┤
│ Bottom Drawer                                                │
│ Prompt Preview | AI Output | Logs | Reports                 │
└─────────────────────────────────────────────────────────────┘

```

The graph remains the central focus of the application.

---

# Header

Contains:

- Repository name
- Current branch
- Active conflict
- Search
- Theme toggle
- Export button
- Settings

The header should remain fixed while scrolling.

---

# Repository Navigator

Purpose

Provide hierarchical navigation through the repository.

Contents

- Repository
- Folders
- Files
- Conflicts
- Search
- Filters

Nodes should expand similarly to a file explorer.

---

# Semantic Explorer

The central Cytoscape canvas.

Responsibilities

- Visualize semantic relationships
- Expand and collapse nodes
- Highlight conflicts
- Display dependency paths
- Animate graph transitions

The graph should always remain interactive.

---

# Inspector Panel

Selecting any node opens the Inspector.

Repository

Displays

- Branches
- Commits
- Statistics

File

Displays

- Path
- Language
- Functions
- Imports

Function

Displays

- Signature
- Dependencies
- Callers
- Callees
- Variables
- Source preview

Conflict

Displays

- Files involved
- Functions involved
- Prompt size
- AI status

---

# Bottom Drawer

Contains four tabs.

## Prompt Preview

Displays exactly what will be sent to the AI.

---

## AI Recommendation

Displays

- Summary
- Suggested merge
- Reasoning

---

## Logs

Displays backend events.

Useful for debugging.

---

## Reports

Displays generated analysis.

---

# User Workflow

Typical workflow:

1. Open repository
2. Select conflict
3. Explore semantic graph
4. Inspect affected functions
5. Review prompt preview
6. Generate AI recommendation
7. Review explanation
8. Apply merge manually

The UI should naturally guide the user through this sequence.

---

# Color Palette

Use a neutral dark theme by default.

Background

- #0F172A

Surface

- #1E293B

Primary

- #3B82F6

Success

- #22C55E

Warning

- #F59E0B

Error

- #EF4444

Text

- #F8FAFC

Muted Text

- #94A3B8

Color should communicate state, not decoration.

---

# Typography

Primary Font

Inter

Fallback

System UI

Headings

600–700 weight

Body

400–500 weight

Monospace

JetBrains Mono

Used for

- Source code
- Signatures
- Logs

---

# Icons

Recommended

Lucide React

Reasons

- Lightweight
- Modern
- Consistent
- Open Source

Avoid mixing icon libraries.

---

# Animations

Animations should be subtle.

Recommended durations

100–250 ms

Examples

- Panel expansion
- Node expansion
- Drawer opening
- Inspector updates

Avoid unnecessary motion.

---

# Graph Interaction

Single Click

Select

Double Click

Expand

Right Click

Context menu

Mouse Wheel

Zoom

Drag

Pan

Keyboard

F

Focus selected node

Ctrl + F

Search

Esc

Clear selection

---

# Visual States

Nodes should visually communicate meaning.

Repository

Default

Folder

Default

File

Blue

Function

Purple

Conflict

Red

Prompt Context

Green outline

Collapsed

Gray

Selected

Primary highlight

---

# Empty States

If no repository is loaded:

Display

"Open a Git repository to begin analysis."

If no conflicts exist:

Display

"No merge conflicts detected."

Empty states should guide the user.

---

# Loading States

Show progress for

- Repository analysis
- Parsing
- Graph generation
- AI request

Avoid blocking the interface.

---

# Accessibility

Minimum contrast ratio: WCAG AA.

Keyboard navigation supported.

Visible focus indicators.

Color is never the only indicator of state.

---

# Responsive Design

Desktop is the primary target.

Tablet support is optional.

Mobile is not part of the MVP.

---

# Design Inspirations

The interface draws inspiration from:

- Visual Studio Code
- GitHub Desktop
- Linear
- Raycast
- Postman
- Obsidian Graph View

The goal is to create a familiar experience for developers while introducing a new way to explore semantic relationships.

---

# Future UI Enhancements

Potential future additions include:

- Split-screen source comparison
- Time-travel through commit history
- Live collaboration
- AI chat assistant
- Minimap for large graphs
- Multiple graph layouts
- Custom themes

These features are outside the scope of the MVP.

---

# Summary

The MergeGraph AI interface is designed to make complex merge conflicts understandable through progressive exploration rather than overwhelming visualization.

The graph acts as the primary workspace, supported by contextual navigation, detailed inspection, and transparent AI interactions.

The overall experience should feel closer to a professional IDE than a traditional graph viewer.

---

**End of Design Specification**