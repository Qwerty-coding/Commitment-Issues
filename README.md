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
- For Gemini: a Gemini API key exported as `GEMINI_API_KEY`
- For local Qwen or Llama: Ollama installed and running locally
- Network access for the provider you choose

## Build and run

The native Tree-sitter dependencies are best built inside Docker, where the required C toolchain is already installed.

From the repository root, build and run the container with:

```bash
docker build -t commitment-issues .
docker run --rm -it commitment-issues --help
```

If you want to run the CLI locally, use the containerized build path instead of relying on the host environment.

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

## Docker health check

The container now includes a Docker `HEALTHCHECK` that runs the CLI help command every 30 seconds.

The image also installs the native C toolchain needed by Tree-sitter's CGO-based bindings, so it can be built and run consistently in Docker by other users.

## AI provider configuration

The resolution backend is now routed through a small provider interface. By default it uses Gemini, but you can switch it with the `AI_PROVIDER` environment variable:

```bash
export AI_PROVIDER=gemini
```

For local model support, set `AI_PROVIDER=qwen` or `AI_PROVIDER=llama` and make sure Ollama is running locally. The code uses these default local models:

- `qwen2.5-coder`
- `llama3.1`

If you have a different local Ollama host, set `OLLAMA_HOST` before running the command.

This makes it easier to plug in another provider later without changing the core merge workflow.

Why it exists:
- it lets Docker know whether the container is still healthy,
- it fails fast if the entrypoint is broken or the binary cannot start,
- it gives you a simple way to monitor the service from `docker ps` or container orchestration tools.

The check is defined in [Dockerfile](Dockerfile) and uses:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/mergetool", "--help"] >/dev/null 2>&1 || exit 1
```

## Notes

- This repository is a prototype and the current implementation is intentionally lightweight.
- The `diff` and `setup` commands are not yet fully implemented.
- The merge pipeline now uses Tree-sitter parsers selected by file extension, so it can work across multiple languages.
- The system relies on Gemini for final conflict resolution, so the API key must be available in the environment.
