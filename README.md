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
- `go run main.go serve --port :8080 [path]`
  - Start the graph server and optionally scan the target path.

### Backend environment

The backend currently supports AI integration via providers such as `ollama`, `gemini`, and `groq`.

Environment variables:

- `OLLAMA_BASE_URL` - Base URL for Ollama API access.
- `AI_API_KEY` - API key for AI providers when required.

### API endpoints

When the backend server is running, the following API endpoints are available:

- `GET /api/repository` - repository metadata
- `GET /api/graph` - merged AST/semantic graph data
- `GET /api/analysis` - all file analyses or `?file=<path>` for a specific file
- `GET /api/prompt` - prompt context list or `?file=<path>` for a specific conflict
- `GET /api/prompt/statistics` - prompt statistics list or `?file=<path>`
- `GET /api/suggestions` - AI suggestions list or `?file=<path>`

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
