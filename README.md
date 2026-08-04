# Merge Conflicts Resolver

Merge Conflicts Resolver is a prototype AST-guided conflict resolution tool for Rust source files. It combines Tree-sitter parsing, lightweight structural diffing, and Gemini-based resolution to produce a merged version of a conflicted file.

The project is currently organized as a Cobra-based CLI with an internal engine package that handles parsing, payload construction, conflict resolution, and syntax validation.

## Current architecture

### CLI layer
The command layer lives in the [cmd](cmd) directory and exposes the following entry points:

- `diff` – a placeholder command for showing structural AST diffs.
- `merge` – the main merge-driver style workflow.
- `resolve` – a more explicit resolution workflow for base/local/remote input files.
- `setup` – a placeholder for registering the tool as a Git merge driver.

### Engine layer
The core logic lives in the [internal/engine](internal/engine) package:

- `ast.go` – parses files with Tree-sitter, builds a reduced AST payload, and extracts top-level Rust nodes.
- `checker.go` – contains deterministic heuristics for simple auto-resolutions.
- `gaurd.go` – validates generated code with a Tree-sitter parser before writing it to disk.
- `isolate.go` – converts AST changes into contextual operation payloads with function or type scope.
- `llm.go` – sends the reduced TOON payload to Gemini and returns resolved code.
- `types.go` – defines the AST operation and conflict payload structures.

### Data flow
1. Parse the base, local, and remote Rust files with Tree-sitter.
2. Extract the relevant top-level AST nodes and compare them.
3. Convert the structural changes into a compact operation payload.
4. Send the payload to Gemini with local source context.
5. Validate the returned code syntactically.
6. Write the resolved output to the requested file path.

## Prerequisites

- Go 1.23 or newer
- A Gemini API key exported as `GEMINI_API_KEY`
- Network access for Gemini API calls

## Build and run

From the repository root:

```bash
go build ./...
```

Run the CLI with:

```bash
go run . <command> [flags]
```

## Commands

### `diff`

A placeholder command for AST diff inspection.

```bash
go run . diff
```

### `merge`

The primary merge-style workflow. It requires base, local, remote, and output paths.

```bash
go run . merge \
  --base testfiles/base.rs \
  --local testfiles/local.rs \
  --remote testfiles/remote.rs \
  --output ./resolved.rs
```

What this command does:
- reads the local and remote files,
- builds an AST-based conflict payload,
- asks Gemini to resolve the conflict,
- validates the result,
- writes the merged output to the output path.

### `resolve`

A direct resolution workflow with the same core behavior as `merge`, but exposed as a more explicit CLI command.

```bash
go run . resolve \
  --base testfiles/base.rs \
  --local testfiles/local.rs \
  --remote testfiles/remote.rs \
  --output ./resolved.rs
```

### `setup`

A placeholder command for registering the tool as a Git merge driver.

```bash
go run . setup
```

## Example usage

```bash
export GEMINI_API_KEY="your-api-key"

go run . resolve \
  --base testfiles/base.rs \
  --local testfiles/local.rs \
  --remote testfiles/remote.rs \
  --output ./resolved.rs
```

## Notes

- This repository is a prototype and the current implementation is intentionally lightweight.
- The `diff` and `setup` commands are not yet fully implemented.
- The merge pipeline is currently focused on Rust files and uses Tree-sitter’s Rust grammar.
- The system relies on Gemini for final conflict resolution, so the API key must be available in the environment.
