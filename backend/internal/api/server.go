package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ai "CommitIssues/internal/ai"
	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/runstate"
	semantic "CommitIssues/internal/semantic"
)

var (
	repoMetadataMu sync.RWMutex
	repoMetadata   RepositoryMetadata

	suggestionMu    sync.Mutex
	suggestionCache = make(map[string][]SuggestionItem)
)

type RepositoryMetadata struct {
	Name           string `json:"name"`
	CurrentBranch  string `json:"currentBranch"`
	IncomingBranch string `json:"incomingBranch"`
}

type SuggestionItem struct {
	File       string                  `json:"file"`
	Collision  semantic.DiffItem       `json:"collision"`
	Resolution ai.AIResolutionResponse `json:"resolution"`
}

type suggestionsResponse struct {
	Success bool             `json:"success"`
	Data    []SuggestionItem `json:"data"`
}

func SetRepositoryMetadata(meta RepositoryMetadata) {
	repoMetadataMu.Lock()
	defer repoMetadataMu.Unlock()
	repoMetadata = meta
}

func getRepositoryMetadata() RepositoryMetadata {
	repoMetadataMu.RLock()
	defer repoMetadataMu.RUnlock()
	return repoMetadata
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

// StartGraphServer serves the API and static frontend using the supplied
// run-scoped state. A nil run is treated as an empty run.
func StartGraphServer(run *runstate.Run, addr string) {
	if run == nil {
		run = runstate.NewRun()
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
		writeJSON(w, http.StatusOK, struct {
			Success bool               `json:"success"`
			Data    RepositoryMetadata `json:"data"`
		}{Success: true, Data: getRepositoryMetadata()})
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, http.StatusOK, standardResponse{Success: true, Message: "", Data: run.MergedGraphDTO()})
	})

	mux.HandleFunc("/api/analysis", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		file := r.URL.Query().Get("file")
		if file != "" {
			analysis, ok := run.GetAnalysis(file)
			if !ok {
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
			ctx, ok := run.GetPromptContext(file)
			if !ok {
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
		file := r.URL.Query().Get("file")

		items, err := generateSuggestions(run, file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SUGGESTION_ERROR", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, suggestionsResponse{Success: true, Data: items})
	})

	for _, path := range []string{"/api/graph/expand", "/api/graph/collapse", "/api/graph/focus"} {
		name := strings.TrimPrefix(path, "/api/graph/")
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", fmt.Sprintf("Graph %s is not implemented yet.", name))
		})
	}

	fmt.Printf("listening on http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Printf("Graph server error: %v\n", err)
	}
}

func generateSuggestions(run *runstate.Run, file string) ([]SuggestionItem, error) {
	suggestionMu.Lock()
	defer suggestionMu.Unlock()
	if items, ok := suggestionCache[file]; ok && len(items) > 0 {
		return items, nil
	}

	analyses := run.AllAnalyses()
	if len(analyses) == 0 {
		return nil, fmt.Errorf("no analysis available to generate suggestions")
	}

	if file != "" {
		found := false
		for _, analysis := range analyses {
			if analysis.File == file {
				analyses = []promptcontext.FileAnalysis{analysis}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no analysis found for %s", file)
		}
	}

	resolver, err := ai.GetResolver(ai.AIConfig{
		Provider: "ollama",
		Model:    "qwen2:1.5b",
		BaseURL:  os.Getenv("OLLAMA_BASE_URL"),
	})
	if err != nil {
		return nil, err
	}

	items := make([]SuggestionItem, 0)
	for _, analysis := range analyses {
		for _, collision := range analysis.SmartDiff.Collisions {
			resolution, err := resolver.ResolveCollision(context.Background(), collision, analysis.PromptContext)
			if err != nil {
				return nil, fmt.Errorf("failed to generate suggestion for %s: %w", analysis.File, err)
			}
			items = append(items, SuggestionItem{
				File:       analysis.File,
				Collision:  collision,
				Resolution: *resolution,
			})
		}
	}

	if file != "" {
		suggestionCache[file] = items
	}
	return items, nil
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

type promptResponse struct {
	Success bool                          `json:"success"`
	Data    promptcontext.PromptContextIR `json:"data"`
}

type promptListResponse struct {
	Success bool                            `json:"success"`
	Data    []promptcontext.PromptContextIR `json:"data"`
}
