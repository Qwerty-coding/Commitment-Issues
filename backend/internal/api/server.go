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
	graph "CommitIssues/internal/graph"
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

func StartGraphServer(addr string) {
	mux := http.NewServeMux()

	staticPath := filepath.Join("..", "frontend", "dist")
	if _, err := os.Stat(staticPath); err == nil {
		fileServer := http.FileServer(http.Dir(staticPath))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				// API routes are handled separately.
				w.WriteHeader(http.StatusNotFound)
				return
			}

			assetPath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
			if r.URL.Path != "/" {
				if info, err := os.Stat(assetPath); err == nil && !info.IsDir() {
					fileServer.ServeHTTP(w, r)
					return
				}
			}

			// Serve index.html for client-side routes.
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
		writeJSON(w, http.StatusOK, standardResponse{Success: true, Message: "", Data: graph.MergedGraphDTO()})
	})

	mux.HandleFunc("/api/analysis", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		file := r.URL.Query().Get("file")
		if file != "" {
			analysis, ok := promptcontext.GetAnalysis(file)
			if !ok {
				writeJSON(w, http.StatusNotFound, errorResponse{Success: false, Error: struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{Code: "ANALYSIS_NOT_FOUND", Message: fmt.Sprintf("No analysis generated yet for %s", file)}})
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
		}{Success: true, Data: promptcontext.AllAnalyses()})
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
			ctx, ok := promptcontext.GetPromptContext(file)
			if !ok {
				writeJSON(w, http.StatusNotFound, errorResponse{Success: false, Error: struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{Code: "CONFLICT_NOT_FOUND", Message: fmt.Sprintf("No prompt context generated yet for %s", file)}})
				return
			}
			writeJSON(w, http.StatusOK, promptResponse{Success: true, Data: ctx})
			return
		}

		writeJSON(w, http.StatusOK, promptListResponse{Success: true, Data: promptcontext.AllPromptContexts()})
	})

	mux.HandleFunc("/api/prompt/statistics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")

		file := r.URL.Query().Get("file")
		if file != "" {
			s, ok := promptcontext.GetPromptStatistics(file)
			if !ok {
				writeJSON(w, http.StatusNotFound, errorResponse{Success: false, Error: struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{Code: "CONFLICT_NOT_FOUND", Message: fmt.Sprintf("No statistics generated yet for %s", file)}})
				return
			}
			writeJSON(w, http.StatusOK, promptStatsResponse{Success: true, Data: s})
			return
		}

		writeJSON(w, http.StatusOK, promptStatsListResponse{Success: true, Data: promptcontext.AllPromptStatistics()})
	})

	mux.HandleFunc("/api/suggestions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		file := r.URL.Query().Get("file")

		items, err := generateSuggestions(file)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Success: false, Error: struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}{Code: "SUGGESTION_ERROR", Message: err.Error()}})
			return
		}

		writeJSON(w, http.StatusOK, suggestionsResponse{Success: true, Data: items})
	})

	mux.HandleFunc("/api/graph/expand", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, http.StatusNotImplemented, errorResponse{Success: false, Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: "NOT_IMPLEMENTED", Message: "Graph expansion is not implemented yet."}})
	})

	mux.HandleFunc("/api/graph/collapse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, http.StatusNotImplemented, errorResponse{Success: false, Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: "NOT_IMPLEMENTED", Message: "Graph collapse is not implemented yet."}})
	})

	mux.HandleFunc("/api/graph/focus", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, http.StatusNotImplemented, errorResponse{Success: false, Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: "NOT_IMPLEMENTED", Message: "Graph focus is not implemented yet."}})
	})

	fmt.Printf("Graph server listening on http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Printf("Graph server error: %v\n", err)
	}
}

func generateSuggestions(file string) ([]SuggestionItem, error) {
	suggestionMu.Lock()
	defer suggestionMu.Unlock()
	if items, ok := suggestionCache[file]; ok && len(items) > 0 {
		return items, nil
	}

	analyses := promptcontext.AllAnalyses()
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

type promptStatsResponse struct {
	Success bool                              `json:"success"`
	Data    promptcontext.PromptStatisticsDTO `json:"data"`
}

type promptStatsListResponse struct {
	Success bool                                `json:"success"`
	Data    []promptcontext.PromptStatisticsDTO `json:"data"`
}

type promptResponse struct {
	Success bool                          `json:"success"`
	Data    promptcontext.PromptContextIR `json:"data"`
}

type promptListResponse struct {
	Success bool                            `json:"success"`
	Data    []promptcontext.PromptContextIR `json:"data"`
}
