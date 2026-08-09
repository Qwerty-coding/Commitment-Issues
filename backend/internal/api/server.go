package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	promptcontext "CommitIssues/internal/context"
	graph "CommitIssues/internal/graph"
)

type promptResponse struct {
	Success bool                          `json:"success"`
	Data    promptcontext.PromptContextIR `json:"data"`
}

type promptListResponse struct {
	Success bool                            `json:"success"`
	Data    []promptcontext.PromptContextIR `json:"data"`
}

type promptStatsResponse struct {
	Success bool                              `json:"success"`
	Data    promptcontext.PromptStatisticsDTO `json:"data"`
}

type promptStatsListResponse struct {
	Success bool                                `json:"success"`
	Data    []promptcontext.PromptStatisticsDTO `json:"data"`
}

type standardResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Data    graph.GraphDTO `json:"data"`
}

type errorResponse struct {
	Success bool `json:"success"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func StartGraphServer(addr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		writeJSON(w, http.StatusOK, standardResponse{Success: true, Message: "", Data: graph.MergedGraphDTO()})
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
