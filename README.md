# Commitment Issues

AST-guided Git merge conflict resolver for Rust source files. The tool parses base, local, and remote versions with Tree-sitter, builds a compact structural diff payload, sends it to Gemini in TOON format (Token-Oriented Object Notation) to minimize token usage, validates the merged output, and writes the resolved file.

## How it works

```
base.rs + local.rs + remote.rs
        │
        ▼
  Tree-sitter parse (Rust grammar)
        │
        ▼
  Top-level AST diff (fn, struct, impl, enum, …)
        │
        ▼
  Contextual operations (scope + local/remote snippets)
        │
        ▼
  TOON payload  ──►  Gemini API  ──►  merged source
        │
        ▼
  Syntax guard (Tree-sitter)  ──►  output file
```

### Pipeline phases

| Phase | Component | Description |
|-------|-----------|-------------|
| 1 | `ast.go` | Parse base, local, and remote files; reject files with syntax errors |
| 2 | `ast.go` | Collect and compare top-level AST nodes by name |
| 3 | `isolate.go` | Map each diff to an `ASTOperation` with enclosing scope |
| 4 | `llm.go` | Serialize payload as TOON and call Gemini for resolution |
| 5 | `gaurd.go` | Parse LLM output with Tree-sitter; write only if syntactically valid |

Deterministic auto-resolution heuristics live in `checker.go` (function reordering, identifier renames, operator swaps) and are available for future integration before the LLM step.

## TOON payload format

Conflict data is sent to Gemini as TOON instead of YAML or JSON. TOON uses tabular arrays so field names are declared once, reducing prompt tokens by roughly 30–50% for uniform operation lists.

Example payload for a single conflicting function:

```toon
f: testfiles/local.rs
ops[1]{a,s,l,r}:
  UPDATE,fn main,"fn main() { let x = 5; ... }","fn main() { let x = 10; let y = 20; ... }"
```

| Key | Meaning |
|-----|---------|
| `f` | File path |
| `ops` | List of conflicting operations |
| `a` | Action: `UPDATE`, `INSERT`, or `DELETE` |
| `s` | Enclosing scope (e.g. `fn main`, `type Foo`) |
| `l` | Local code snippet |
| `r` | Remote code snippet |

## Project layout

```
.
├── cmd/                  # Cobra CLI commands
│   ├── merge.go          # Git merge-driver workflow
│   ├── resolve.go        # Explicit resolution workflow
│   ├── diff.go           # Placeholder AST diff viewer
│   └── setup.go          # Placeholder Git driver registration
├── internal/engine/      # Core merge engine
│   ├── ast.go            # Tree-sitter parsing and payload construction
│   ├── checker.go        # Deterministic conflict heuristics
│   ├── gaurd.go          # Post-LLM syntax validation
│   ├── isolate.go        # Scope-aware operation mapping
│   ├── llm.go            # TOON serialization and Gemini integration
│   └── types.go          # ConflictPayload and ASTOperation types
├── testfiles/            # Sample base/local/remote Rust files
├── Dockerfile            # Container image (binary: /mergetool)
└── main.go
```

## Prerequisites

- Go 1.23+
- C compiler (required for Tree-sitter CGO bindings)
- Gemini API key set as `GEMINI_API_KEY`
- Network access for Gemini API calls

## Build and run

```bash
# Build
go build ./...

# Run a command
go run . <command> [flags]
```

## Commands

### `resolve`

Recommended entry point. Reads base/local/remote files, builds an AST diff payload, resolves via Gemini, validates, and writes output.

```bash
export GEMINI_API_KEY="your-api-key"

go run . resolve \
  --base testfiles/base.rs \
  --local testfiles/local.rs \
  --remote testfiles/remote.rs \
  --output testfiles/resolved.rs
```

Flags:

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--base` | `-b` | yes | Common ancestor file |
| `--local` | `-l` | yes | Local (ours) version |
| `--remote` | `-r` | yes | Remote (theirs) version |
| `--output` | `-o` | yes | Path for merged result |

### `merge`

Git merge-driver style workflow. Uses the same AST pipeline as `resolve`.

```bash
go run . merge \
  --base testfiles/base.rs \
  --local testfiles/local.rs \
  --remote testfiles/remote.rs \
  --output testfiles/resolved.rs
```

### `diff`

Placeholder for a future structural AST diff viewer.

```bash
go run . diff
```

### `setup`

Placeholder for registering the tool as a Git merge driver.

```bash
go run . setup
```

## Docker

The Docker image builds the CLI as `/mergetool` with CGO enabled for Tree-sitter.

```bash
# Build image
docker build -t mergetool .

# Run resolve inside container
docker run --rm \
  -e GEMINI_API_KEY="your-api-key" \
  -v "${PWD}:/app" \
  -w /app \
  mergetool resolve \
    -b testfiles/base.rs \
    -l testfiles/local.rs \
    -r testfiles/remote.rs \
    -o testfiles/resolved.rs
```

On Windows PowerShell:

```powershell
docker run --rm `
  -e GEMINI_API_KEY="your-api-key" `
  -v ${PWD}:/app `
  -w /app `
  mergetool resolve `
    -b testfiles/base.rs `
    -l testfiles/local.rs `
    -r testfiles/remote.rs `
    -o testfiles/resolved.rs
```

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GEMINI_API_KEY` | yes | Google Gemini API key for conflict resolution |


## Limitations

- Rust only — uses Tree-sitter's Rust grammar; other languages are not supported yet.
- Prototype — top-level node diffing only; nested or intra-function conflicts may not be isolated precisely.
- LLM-dependent — final merge quality depends on Gemini; invalid output is rejected by the syntax guard but not auto-retried.
- Placeholder commands — `diff` and `setup` are not fully implemented.
- Full local context — the entire local file is still sent alongside the TOON diff for additional context, which can dominate token usage on large files.


