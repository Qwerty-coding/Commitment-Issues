package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	ai "CommitIssues/internal/ai"
	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/runstate"
	"CommitIssues/internal/suggestions"
)

// RepositoryMetadata and SuggestionItem are aliases to their run-scoped
// counterparts so the JSON shape stays identical while storage lives on the
// run rather than in package-level globals.
type RepositoryMetadata = runstate.RepositoryMetadata

type SuggestionItem = runstate.Suggestion

type suggestionsResponse struct {
	Success bool              `json:"success"`
	Data    []SuggestionItem  `json:"data"`
	Meta    *suggestions.Meta `json:"meta,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Success: false, Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
}

// ShutdownTimeout bounds how long in-flight requests are allowed to drain
// during a graceful shutdown.
const ShutdownTimeout = 10 * time.Second

// StartGraphServer serves the API and static frontend using the supplied
// run-scoped state and bounded run history. A nil run is treated as an empty
// run; a nil history disables the history endpoint's data (it returns an
// empty list rather than failing).
//
// The server shuts down gracefully when ctx is cancelled: it stops accepting
// new connections and waits (bounded by ShutdownTimeout) for in-flight
// requests to finish. Passing a nil ctx is treated as context.Background().
func StartGraphServer(ctx context.Context, run *runstate.Run, history *runstate.History, addr string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil {
		run = runstate.NewRun()
	}
	if history == nil {
		history = runstate.NewHistory(runstate.DefaultHistoryLimit)
	}

	mux := NewHandler(run, history)
	srv := &http.Server{Addr: addr, Handler: mux}

	fmt.Printf("listening on http://localhost%s\n", addr)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		// Graceful shutdown: drain in-flight requests within a bounded window.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Printf("Graph server shutdown error: %v\n", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("Graph server error: %v\n", err)
		}
	}
}

// NewHandler builds the API + static-file handler for a run and its bounded
// history. It is exported so handler behavior can be verified through the
// real HTTP interface without starting a listener.
func NewHandler(run *runstate.Run, history *runstate.History) http.Handler {
	if run == nil {
		run = runstate.NewRun()
	}
	if history == nil {
		history = runstate.NewHistory(runstate.DefaultHistoryLimit)
	}

	mux := http.NewServeMux()

	staticPath := filepath.Join("..", "frontend", "dist")
	if _, err := os.Stat(staticPath); err == nil {
		fileServer := http.FileServer(http.Dir(staticPath))
		fmt.Printf("Serving static files from %s\n", staticPath)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "API route not found: "+r.URL.Path)
				return
			}

			assetPath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
			if r.URL.Path != "/" {
				if info, statErr := os.Stat(assetPath); statErr == nil && !info.IsDir() {
					fileServer.ServeHTTP(w, r)
					return
				}
			}

			http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "CommitIssues Graph Server running. Use /api endpoints or build the frontend into ../frontend/dist.")
		})
	}

	mux.HandleFunc("/api/repository", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// CacheStats is surfaced alongside repository metadata so operators can
		// observe AST cache hit/miss behaviour without an extra endpoint.
		writeJSON(w, http.StatusOK, struct {
			Success    bool               `json:"success"`
			Data       RepositoryMetadata `json:"data"`
			CacheStats interface{}        `json:"cacheStats,omitempty"`
		}{Success: true, Data: run.GetRepositoryMetadata(), CacheStats: run.GetCacheStats()})
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, http.StatusOK, standardResponse{Success: true, Message: "", Data: run.MergedGraphDTO()})
	})

	mux.HandleFunc("/api/analysis", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		file := r.URL.Query().Get("file")
		if file != "" {
			analysis, ok := run.FindAnalysis(file)
			if !ok {
				// Note: for multi-repository runs, a bare file name may match more
				// than one repository. Choose the first deterministic match for
				// backward compatibility with the frontend's file-only queries.
				writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", fmt.Sprintf("No analysis generated yet for %s", file))
				return
			}
			writeJSON(w, http.StatusOK, struct {
				Success bool                       `json:"success"`
				Data    promptcontext.FileAnalysis `json:"data"`
			}{Success: true, Data: analysis})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Success bool                         `json:"success"`
			Data    []promptcontext.FileAnalysis `json:"data"`
		}{Success: true, Data: run.AllAnalyses()})
	})

	mux.HandleFunc("/api/prompt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")

		file := r.URL.Query().Get("file")
		if r.Method == http.MethodPost {
			var body struct {
				File string `json:"file"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.File != "" {
				file = body.File
			}
		}

		if file != "" {
			ctx, ok := run.FindPromptContext(file)
			if !ok {
				// Note: for multi-repository runs, a bare file name may match more
				// than one repository. Choose the first deterministic match for
				// backward compatibility with the frontend's file-only queries.
				writeError(w, http.StatusNotFound, "CONFLICT_NOT_FOUND", fmt.Sprintf("No prompt context generated yet for %s", file))
				return
			}
			writeJSON(w, http.StatusOK, promptResponse{Success: true, Data: ctx})
			return
		}

		writeJSON(w, http.StatusOK, promptListResponse{Success: true, Data: run.AllPromptContexts()})
	})

	mux.HandleFunc("/api/suggestions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		handleSuggestions(w, r, run, history)
	})

	mux.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, http.StatusOK, historyResponse{
			Success: true,
			Data:    history.List(),
		})
	})

	for _, path := range []string{"/api/graph/expand", "/api/graph/collapse", "/api/graph/focus"} {
		name := strings.TrimPrefix(path, "/api/graph/")
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", fmt.Sprintf("Graph %s is not implemented yet.", name))
		})
	}

	// Phase 5: resolution endpoints and analysis refresh.
	registerResolutionHandlers(mux, run)
	handleAnalysisRefresh(mux, run)

	// Phase 4B: commit and repository comparison endpoints.
	registerCompareHandlers(mux, run)

	return mux
}

// handleSuggestions generates run-scoped AI suggestions using the
// request-scoped context (no context.Background()): client disconnects
// cancel generation, per-request timeouts are honored, and provider/model
// metadata is returned alongside the suggestions.
func handleSuggestions(w http.ResponseWriter, r *http.Request, run *runstate.Run, history *runstate.History) {
	ctx := r.Context()
	file := r.URL.Query().Get("file")

	generator := &suggestions.Generator{
		Cfg:     ai.ConfigFromEnv(),
		History: history,
	}

	var (
		result suggestions.Result
		err    error
	)
	if file != "" {
		result, err = generator.GenerateForFile(ctx, run, "", file)
	} else {
		result, err = generator.GenerateForRun(ctx, run)
	}

	// The client is gone: writing a response is pointless and misleading.
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.Canceled) {
			return
		}
		writeError(w, http.StatusGatewayTimeout, "TIMEOUT", "suggestion generation exceeded its deadline")
		return
	}

	if err != nil {
		writeSuggestionError(w, err)
		return
	}

	meta := result.Meta
	writeJSON(w, http.StatusOK, suggestionsResponse{Success: true, Data: result.Suggestions, Meta: &meta})
}

// writeSuggestionError maps typed AI failures to actionable HTTP responses.
// Configuration/provider problems are 503 (the frontend can explain them),
// missing analysis is 404, everything else is 500. API keys never appear in
// messages (all ai errors are redacted at construction).
func writeSuggestionError(w http.ResponseWriter, err error) {
	typed := ai.AsError(err)
	switch typed.Code {
	case ai.CodeProviderUnavailable, ai.CodeModelMissing, ai.CodeAPIKeyMissing,
		ai.CodeProviderUnsupported, ai.CodeConfigInvalid, ai.CodeBaseURLInvalid:
		writeError(w, http.StatusServiceUnavailable, string(typed.Code), typed.Error())
		return
	case ai.CodeTimeout:
		writeError(w, http.StatusGatewayTimeout, string(typed.Code), typed.Error())
		return
	}

	if strings.Contains(err.Error(), "no analysis") {
		writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "SUGGESTION_ERROR", typed.Error())
}

type standardResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type errorResponse struct {
	Success bool `json:"success"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type historyResponse struct {
	Success bool                  `json:"success"`
	Data    []runstate.RunHistory `json:"data"`
}

type promptResponse struct {
	Success bool                          `json:"success"`
	Data    promptcontext.PromptContextIR `json:"data"`
}

type promptListResponse struct {
	Success bool                            `json:"success"`
	Data    []promptcontext.PromptContextIR `json:"data"`
}
