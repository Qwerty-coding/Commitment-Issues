# Commitment-Issues

Commitment-Issues is a Git merge conflict analysis and resolution tool that combines AST-driven parsing, semantic diff visualization, and AI-powered suggestions. It includes:

- A Go backend for repository scanning, conflict analysis, AST diff extraction, prompt context generation, and AI resolution.
- A React + Vite frontend for graph visualization, file history, conflict suggestions, and prompt metrics.
- A small sample `conflicts/` folder and report export support for offline analysis.

## Repository Structure

- `backend/` - Go backend application using Cobra CLI and an HTTP server.
- `frontend/` - Vite React frontend application.
- `conflicts/` - Example conflicted files for local testing.
- `reports/` - Generated report JSON files from repository analysis.
- `cli/` - Minimal Go CLI module scaffold.

## Backend

The backend is built with Go and provides both command-line and HTTP server modes.

### Main backend commands

From `backend/`, use:

- `go run main.go scan [path]`
  - Scan a folder or repository tree for Git merge conflicts.
- `go run main.go analyze [path]`
  - Analyze conflicted files and generate AST diff output.
- `go run main.go resolve [path]`
  - Use AI providers to resolve merge collisions.
- `go run main.go compare --ours <ref> --theirs <ref> [--base <ref>] [--repo .] [--json]`
  - Compare two commits/branches (or two repositories with `--other-repo`) and report
    hypothetical merge conflicts — merge base, add/delete/rename detection, and AST
    structural collisions — without mutating the working tree.
- `go run main.go bench [--out ../docs/metrics.md] [--json <path>] [--runs 5]`
  - Run the metrics harness (JSON-vs-TOON token cost, AST cache latency, E2E analyze
    timing, README context cost, prompt budget trimming, suggestion reuse) and write
    a dated report; the latest numbers are committed in `docs/metrics.md`.
- `go run main.go serve --port :8080 [path]`
  - Start the graph server and optionally scan the target path.

### Backend environment

The backend supports AI integration via `ollama`, `gemini`, and `groq`. **Ollama is the default
provider** and `qwen2.5-coder:7b` is the documented default Ollama model; `resolve` and the Suggestions
API are the only AI-invoking paths (`scan`, `analyze` and `serve` are AI-free).

Configuration precedence is: **CLI flags > environment variables > provider defaults**. Nothing is
hard-coded and providers/models are never switched silently.

| Variable | Meaning | Default |
| --- | --- | --- |
| `AI_PROVIDER` | `ollama`, `gemini` or `groq` | `ollama` |
| `AI_MODEL` | Provider model name | `qwen2.5-coder:7b` (ollama), `gemini-3.5-flash`, `llama-3.3-70b-versatile` |
| `AI_BASE_URL` | Provider endpoint (OpenAI-compatible for ollama/groq) | `http://localhost:11434/v1` (ollama) |
| `AI_API_KEY` | API key for hosted providers | — |
| `AI_TIMEOUT` | Per-request timeout (Go duration) | `60s` |
| `AI_RETRIES` | Retries per request for transient failures (429/5xx) | `2` |
| `AI_CONFIDENCE_THRESHOLD` | Suggestion confidence threshold (0-100) | `70` |
| `AI_MAX_PROMPT_BYTES` | Reject prompts larger than this | `524288` |
| `AI_MAX_RESPONSE_BYTES` | Reject provider responses larger than this | `1048576` |
| `AI_RETRY_BACKOFF` | Initial exponential backoff | `500ms` |
| `AI_PAYLOAD_FORMAT` | Encoding for AI-bound payloads (`toon` or `json`) | `toon` |
| `AI_README_CONTEXT` | Include the repo README excerpt in AI prompt context | `true` |
| `AI_README_MAX_BYTES` | Maximum README excerpt size (bytes) | `4096` |
| `AI_TARGET_PROMPT_TOKENS` | Soft prompt-context budget (0 disables) | `8000` |
| `CACHE_DISK_ENABLED` | Enable the persistent AST disk cache | `false` |
| `CACHE_DIR` | Directory for disk cache entries | OS user cache dir (when enabled) |
| `CACHE_MAX_DISK_BYTES` | Maximum on-disk cache size (bytes) | `104857600` |
| `CACHE_MAX_ENTRIES` | Maximum in-memory LRU entries | `10000` |
| `CACHE_MAX_MEMORY_BYTES` | Maximum in-memory cache size (bytes) | `52428800` |
| `HISTORY_PERSIST_PATH` | Opt-in JSON file for durable run history | — (in-memory) |
| `OLLAMA_BASE_URL` | Legacy Ollama base URL (used when `AI_BASE_URL` is unset) | — |

Before generating suggestions the backend performs provider health checks: Ollama reachability
and model presence, API key presence for Gemini/Groq, base URL validity, and clear unsupported
provider errors. API keys are never logged or returned.

### API endpoints

When the backend server is running, the following API endpoints are available:

- `GET /api/repository` - repository metadata
- `GET /api/graph` - merged AST/semantic graph data
- `GET /api/analysis` - all file analyses or `?file=<path>` for a specific file
- `GET /api/prompt` - prompt context list or `?file=<path>` for a specific conflict
- `GET /api/suggestions` - AI suggestions list or `?file=<path>` for a specific file. Responses
  include a `meta` object with `provider`, `model`, `runId`, generation/failure counts and
  structured partial failures. Provider/setup problems return `503` with a typed error code
  (`PROVIDER_UNAVAILABLE`, `MODEL_MISSING`, `API_KEY_MISSING`, ...); per-collision timeouts are
  reported as failed suggestions so successful ones are preserved.
- `GET /api/history` - run history (run ID, repository, timings, files analyzed, collision count,
  provider/model, suggestion outcomes, timeout/cancellation status, error summaries). History is
  bounded in-memory state owned by the server; database persistence is out of scope.
- `GET /api/repository` - repository metadata; also carries `cacheStats` (AST cache
  hits/misses/stores/evictions) when the server captured them.
- `GET|POST /api/compare` - hypothetical commit/repository comparison (same engine as the
  `compare` CLI). `GET` accepts `ours`/`theirs`/`base`/`repositoryRoot` query params; `POST`
  accepts a JSON body with `repositoryRoot`, `baseRef`, `oursRef`, `theirsRef`.
- `GET|POST /api/resolutions` and `/api/resolutions/{id}/{preview|approve|apply|revert|validation}`
  - resolution lifecycle. `POST /api/analysis/refresh` re-analyzes a file after a stale change.

### Testing

From `backend/`:

- `go test ./...` - full backend suite.
- `go test ./internal/eval/` - multi-language golden-fixture harness. Fixtures live in
  `conflicts/fixtures/fixtures.json` and expected collision sets in `conflicts/fixtures/golden.json`;
  regenerate the goldens with `UPDATE_GOLDEN=1 go test ./internal/eval/`.
- `go test -bench BenchmarkColdVsWarm ./internal/cache/` - cold parse vs warm AST-cache hit.

From `frontend/`:

- `npm test` - Vitest + Testing Library component suite.
- `npm run lint` / `npm run build`.

Tier-5 grammars (Kotlin, Swift, Shell/bash, TOML, YAML, CSS, Dockerfile) ship natively in the
pinned `smacker/go-tree-sitter`; Dart, SQL, JSON and Markdown are deliberately deferred (see
`backend/internal/parser/tier5_test.go`).

The backend also serves static frontend assets from `../frontend/dist` when available.

## Frontend

The frontend is a React application built with Vite.

### Frontend commands

From `frontend/`, use:

- `npm install`
- `npm run dev`
- `npm run build`
- `npm run preview`

### Notes

- Build the frontend with `npm run build` to create `frontend/dist`.
- The backend server is configured to serve static files from `frontend/dist` if present.

## Getting Started

### Run the backend server

```bash
cd backend
go run main.go serve --port :8080
```

If you also want to generate or refresh conflict analysis before starting the server:

```bash
cd backend
go run main.go serve --port :8080 ./conflicts
```

### Run the frontend during development

```bash
cd frontend
npm install
npm run dev
```

### Build and serve the frontend with the backend

```bash
cd frontend
npm install
npm run build
cd ../backend
go run main.go serve --port :8080
```

Then open `http://localhost:8080` in a browser.

## Dependencies

### Backend

- Go 1.26.3
- `github.com/spf13/cobra`
- `github.com/smacker/go-tree-sitter`
- `golang.org/x/text`

### Frontend

- React 19
- Vite 8
- ESLint with React plugin

## Notes

- The backend and frontend are currently separate modules; the backend expects a built frontend in `frontend/dist` for static serving.
- The `cli/` folder is a separate Go CLI module scaffold and is not required for the main backend/server workflows.
- Sample conflicting files in `conflicts/` can be used to test scanning and analysis.

## Contact

For more details, inspect the `backend/cmd` subcommands and the HTTP handlers in `backend/internal/api/server.go`.
