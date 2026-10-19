package api

import (
	"net/http"

	git "CommitIssues/internal/git"
	"CommitIssues/internal/runstate"
)

// registerGitRefHandlers exposes the repository's branches, remote branches
// and tags so the compare tab can offer a searchable dropdown instead of
// free-text ref entry (a single typo otherwise fails the whole comparison).
//
//	GET /api/git/branches?repositoryRoot=. -> { success, data: [GitRef...] }
//
// The repository defaults to the run's repository when omitted, and to "." as
// a last resort. A non-git directory yields an empty list rather than an error
// so the UI can show "no refs found" without a hard failure.
func registerGitRefHandlers(mux *http.ServeMux, run *runstate.Run) {
	mux.HandleFunc("/api/git/branches", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET /api/git/branches")
			return
		}

		repo := r.URL.Query().Get("repositoryRoot")
		if repo == "" {
			repo = r.URL.Query().Get("repo")
		}
		if repo == "" && run != nil {
			repo = run.GetRepositoryMetadata().Path
		}
		if repo == "" {
			repo = "."
		}

		refs, err := git.ListRefs(r.Context(), repo)
		if err != nil {
			writeError(w, http.StatusBadRequest, "GIT_REFS_ERROR", err.Error())
			return
		}
		if refs == nil {
			refs = []git.GitRef{}
		}

		writeJSON(w, http.StatusOK, struct {
			Success bool         `json:"success"`
			Data    []git.GitRef `json:"data"`
		}{Success: true, Data: refs})
	})
}
