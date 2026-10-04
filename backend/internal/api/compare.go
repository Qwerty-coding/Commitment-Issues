package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"CommitIssues/internal/compare"
	"CommitIssues/internal/runstate"
)

type compareRequest struct {
	RepositoryRoot string `json:"repositoryRoot"`
	BaseRef        string `json:"baseRef,omitempty"`
	OursRef        string `json:"oursRef"`
	TheirsRef      string `json:"theirsRef"`
}


func registerCompareHandlers(mux *http.ServeMux, run *runstate.Run) {
	mux.HandleFunc("/api/compare", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var opts compare.Options
		defaultRepo := "."

		switch r.Method {
		case http.MethodGet:
			q := r.URL.Query()
			repo := q.Get("repositoryRoot")
			if repo == "" {
				repo = q.Get("repo")
			}
			if repo == "" {
				repo = defaultRepo
			}
			base := q.Get("base")
			if base == "" {
				base = q.Get("baseRef")
			}
			ours := q.Get("ours")
			if ours == "" {
				ours = q.Get("oursRef")
			}
			theirs := q.Get("theirs")
			if theirs == "" {
				theirs = q.Get("theirsRef")
			}

			opts = compare.Options{
				Repo1:     repo,
				BaseRef:   base,
				OursRef:   ours,
				TheirsRef: theirs,
			}

		case http.MethodPost:
			var req compareRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "malformed JSON request body")
				return
			}
			repo := req.RepositoryRoot
			if repo == "" {
				repo = defaultRepo
			}
			opts = compare.Options{
				Repo1:     repo,
				BaseRef:   req.BaseRef,
				OursRef:   req.OursRef,
				TheirsRef: req.TheirsRef,
			}


		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET, POST /api/compare")
			return
		}

		if opts.OursRef == "" || opts.TheirsRef == "" {
			writeError(w, http.StatusBadRequest, "MISSING_REF", "both ours and theirs refs are required")
			return
		}

		result, err := compare.CompareCommits(r.Context(), opts)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "failed to resolve merge base") {
				writeError(w, http.StatusBadRequest, "COMPARE_NO_MERGE_BASE", errMsg)
				return
			}
			if strings.Contains(errMsg, "failed to resolve") || strings.Contains(errMsg, "not found") {
				writeError(w, http.StatusBadRequest, "COMPARE_REF_INVALID", errMsg)
				return
			}
			writeError(w, http.StatusInternalServerError, "COMPARE_ERROR", errMsg)
			return
		}


		writeJSON(w, http.StatusOK, struct {
			Success bool            `json:"success"`
			Data    *compare.Result `json:"data"`
		}{
			Success: true,
			Data:    result,
		})
	})
}
