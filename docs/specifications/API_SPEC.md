# Backend Contract Specification

---

| Field | Value |
|--------|--------|
| Document | API Specification |
| Project | MergeGraph AI |
| Version | 2.0 |
| Protocol | REST |
| Format | JSON |

---

# Purpose

This document defines every API contract between the backend and frontend.

The frontend should never depend on internal implementation details such as Tree-sitter nodes, Git commands, or parser structures.

All communication occurs through stable Data Transfer Objects (DTOs).

This separation ensures that internal algorithms can evolve without breaking the user interface.

---

# API Design Principles

The API follows five principles.

1. Stable contracts.
2. Stateless requests.
3. Explicit errors.
4. Predictable responses.
5. Semantic data over parser data.

---

# Request Flow

```

React UI

↓

REST

↓

API Layer

↓

Core Engine

↓

Response DTO

↓

React State

```

---

# Standard Response

Every endpoint returns

```json
{
  "success": true,
  "message": "",
  "data": {}
}
```

Errors

```json
{
  "success": false,
  "error": {
    "code": "GRAPH_BUILD_FAILED",
    "message": "Unable to build semantic graph."
  }
}
```

---

# Repository Endpoints

## Load Repository

POST

```
/api/repository/load
```

Request

```json
{
  "path": "/projects/demo"
}
```

Response

```json
{
  "repositoryId": "repo1",
  "name": "demo",
  "language": "Go",
  "branches": 4
}
```

---

## Repository Overview

GET

```
/api/repository/{id}
```

Returns

- files
- branches
- commits
- conflicts

---

# Conflict Endpoints

## List Conflicts

GET

```
/api/conflicts
```

Returns

```json
[
  {
    "id": "c1",
    "file": "auth.go",
    "severity": "High"
  }
]
```

---

## Conflict Details

GET

```
/api/conflicts/{id}
```

Returns

- files
- functions
- dependencies
- prompt estimate

---

# Graph Endpoints

## Repository Graph

GET

```
/api/graph
```

Returns

GraphDTO

---

## Expand Node

POST

```
/api/graph/expand
```

Request

```json
{
  "nodeId":"func_login"
}
```

Returns

Only the newly discovered nodes and edges.

The backend should never resend the complete graph.

---

## Collapse Node

POST

```
/api/graph/collapse
```

---

## Focus Conflict

POST

```
/api/graph/focus
```

Returns

Conflict Subgraph.

---

# Prompt Endpoints

## Generate Prompt

POST

```
/api/prompt
```

Returns

PromptContextDTO

---

## Prompt Statistics

GET

```
/api/prompt/statistics
```

Returns

- estimated tokens
- selected nodes
- excluded nodes
- reduction percentage

---

# AI Endpoints

## Generate Recommendation

POST

```
/api/ai/recommend
```

Request

```json
{
    "conflictId":"c1"
}
```

Response

RecommendationDTO

---

# Export

GET

```
/api/export/json
```

GET

```
/api/export/report
```

---

# DTO Models

---

## RepositoryDTO

```json
{
"id":"",
"name":"",
"branches":[],
"conflicts":[]
}
```

---

## FileDTO

```json
{
"id":"",
"name":"",
"path":"",
"language":"",
"functions":[]
}
```

---

## FunctionDTO

```json
{
"id":"",
"name":"",
"signature":"",
"startLine":0,
"endLine":0,
"tokens":0
}
```

---

## NodeDTO

```json
{
"id":"",
"type":"",
"label":"",
"parent":"",
"metadata":{}
}
```

---

## EdgeDTO

```json
{
"id":"",
"source":"",
"target":"",
"type":"CALLS"
}
```

---

## GraphDTO

```json
{
"nodes":[],
"edges":[]
}
```

---

## ConflictDTO

```json
{
"id":"",
"file":"",
"functions":[],
"dependencies":[],
"severity":""
}
```

---

## PromptContextDTO

```json
{
"repositorySummary":"",
"files":[],
"functions":[],
"imports":[],
"context":"",
"estimatedTokens":312
}
```

---

## RecommendationDTO

```json
{
"summary":"",
"reasoning":"",
"changes":[],
"confidence":0.92
}
```

---

# Error Codes

| Code | Meaning |
|------|----------|
| INVALID_REPOSITORY | Repository not found |
| GIT_ERROR | Git command failed |
| PARSER_ERROR | Tree-sitter parsing failed |
| GRAPH_BUILD_FAILED | Semantic graph generation failed |
| CONFLICT_NOT_FOUND | Conflict missing |
| AI_PROVIDER_ERROR | AI request failed |
| EXPORT_ERROR | Report generation failed |

---

# API Versioning

Current version

```
v1
```

Future

```
/api/v2
```

Breaking changes must never modify existing endpoints.

---

# Future APIs

Potential additions include:

- WebSocket updates
- Background indexing
- Multi-user collaboration
- Live graph synchronization
- IDE integration
- GitHub App webhooks

---

# Summary

The Backend Contract provides a stable interface between the Core Engine and the Semantic Explorer.

It hides implementation details while exposing rich semantic information through predictable, versioned APIs.

This separation allows independent evolution of the frontend, backend, and analysis engine without breaking interoperability.

---

**End of Backend Contract Specification**