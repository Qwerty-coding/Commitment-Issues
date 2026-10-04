// Package api resolution handlers — Phase 5.
//
// Endpoints:
//
//	GET  /api/resolutions/{id}
//	POST /api/resolutions/{id}/preview
//	POST /api/resolutions/{id}/approve
//	POST /api/resolutions/{id}/apply
//	POST /api/resolutions/{id}/revert
//	GET  /api/resolutions/{id}/validation
//	POST /api/analysis/refresh
//
// All mutation endpoints require the caller to supply the resolution ID,
// repository root, relative file path, and suggestion revision. Ambiguous
// bare-file mutation requests are rejected.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/apply"
	"CommitIssues/internal/engine"
	"CommitIssues/internal/parser"
	"CommitIssues/internal/patch"
	"CommitIssues/internal/resolutions"
	"CommitIssues/internal/runstate"
	"CommitIssues/internal/safeguard"
	"CommitIssues/internal/suggestions"
	"CommitIssues/internal/validation"
)

// mutationRequest is the body required by all mutation endpoints.
type mutationRequest struct {
	// ResolutionID repeats the URL path ID so callers can verify routing.
	ResolutionID string `json:"resolutionId"`
	// RepositoryRoot is the canonical repository root.
	RepositoryRoot string `json:"repositoryRoot"`
	// File is the repository-relative file path.
	File string `json:"file"`
	// SuggestionRevision is the caller's expected suggestion revision.
	SuggestionRevision int `json:"suggestionRevision"`
	// ExpectedContentHash, when non-empty, must match the working-tree hash.
	ExpectedContentHash string `json:"expectedContentHash,omitempty"`
}

// resolutionResponse wraps a single resolution for API responses.
type resolutionResponse struct {
	Success bool                   `json:"success"`
	Data    resolutions.Resolution `json:"data"`
}

// validationResponse wraps validation events for one resolution.
type validationResponse struct {
	Success bool                          `json:"success"`
	Data    []resolutions.ResolutionEvent `json:"data"`
}

// registerResolutionHandlers registers all /api/resolutions/* routes on mux.
// It is called from NewHandler so the routes are wired alongside the existing
// API surface.
func registerResolutionHandlers(mux *http.ServeMux, run *runstate.Run) {
	// /api/resolutions (list all, filter, or create a proposal)
	mux.HandleFunc("/api/resolutions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet {
			file := r.URL.Query().Get("file")
			repo := r.URL.Query().Get("repository")
			if file != "" {
				if repo != "" {
					writeJSON(w, http.StatusOK, struct {
						Success bool                     `json:"success"`
						Data    []resolutions.Resolution `json:"data"`
					}{Success: true, Data: run.ResolutionsFor(repo, file)})
					return
				}
				var matched []resolutions.Resolution
				for _, res := range run.AllResolutions() {
					if res.File == file {
						matched = append(matched, res)
					}
				}
				writeJSON(w, http.StatusOK, struct {
					Success bool                     `json:"success"`
					Data    []resolutions.Resolution `json:"data"`
				}{Success: true, Data: matched})
				return
			}
			writeJSON(w, http.StatusOK, struct {
				Success bool                     `json:"success"`
				Data    []resolutions.Resolution `json:"data"`
			}{Success: true, Data: run.AllResolutions()})
			return
		}
		if r.Method == http.MethodPost {
			handleCreateResolution(w, r, run)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET, POST /api/resolutions")
	})

	// /api/resolutions/{id}[/{action}]
	mux.HandleFunc("/api/resolutions/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Parse the path: /api/resolutions/{id}[/{action}]
		path := strings.TrimPrefix(r.URL.Path, "/api/resolutions/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "resolution ID required")
			return
		}
		resID := parts[0]
		action := ""
		if len(parts) == 2 {
			action = parts[1]
		}

		switch {
		case r.Method == http.MethodGet && action == "":
			handleGetResolution(w, r, run, resID)

		case r.Method == http.MethodGet && action == "validation":
			handleGetValidation(w, r, run, resID)

		case r.Method == http.MethodPost && action == "preview":
			handlePreview(w, r, run, resID)

		case r.Method == http.MethodPost && action == "approve":
			handleApprove(w, r, run, resID)

		case r.Method == http.MethodPost && action == "apply":
			handleApply(w, r, run, resID)

		case r.Method == http.MethodPost && action == "revert":
			handleRevert(w, r, run, resID)

		default:
			writeError(w, http.StatusNotFound, "NOT_FOUND",
				fmt.Sprintf("no handler for %s /api/resolutions/%s/%s", r.Method, resID, action))
		}
	})
}

// handleCreateResolution explicitly creates or proposes a resolution for an eligible suggestion or collision.
func handleCreateResolution(w http.ResponseWriter, r *http.Request, run *runstate.Run) {
	var req struct {
		SuggestionID   string `json:"suggestionId,omitempty"`
		RepositoryRoot string `json:"repositoryRoot"`
		File           string `json:"file"`
		CollisionKey   string `json:"collisionKey,omitempty"`
		Revision       int    `json:"revision,omitempty"`
		RegionID       string `json:"regionId,omitempty"`
		Replacement    string `json:"replacement,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body: "+err.Error())
		return
	}
	if req.RepositoryRoot == "" || req.File == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "repositoryRoot and file are required")
		return
	}

	absRepo, err := safeguard.VerifyRepository(req.RepositoryRoot)
	if err != nil {
		writeResolutionError(w, err)
		return
	}
	if _, err := safeguard.VerifyFilePath(absRepo, req.File); err != nil {
		writeResolutionError(w, err)
		return
	}

	analysis, found := run.GetAnalysis(absRepo, req.File)
	if !found {
		analysis, found = run.FindAnalysis(req.File)
	}
	if !found {
		writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", fmt.Sprintf("no analysis found for %s", req.File))
		return
	}

	// If linked to an existing suggestion that already has a resolution, return it
	if req.SuggestionID != "" {
		if res, ok := run.FindResolutionBySuggestion(req.SuggestionID); ok {
			writeJSON(w, http.StatusOK, resolutionResponse{Success: true, Data: res})
			return
		}
	}

	rev := req.Revision
	if rev <= 0 {
		rev = 1
	}

	contextHash := ""
	if analysis.RegionContextHashes != nil && req.RegionID != "" {
		contextHash = analysis.RegionContextHashes[req.RegionID]
	}

	initStatus := resolutions.StatusProposed
	if !parser.IsSupportedLanguage(req.File) || req.RegionID == "" {
		initStatus = resolutions.StatusManualReview
	}

	res := resolutions.Resolution{
		ID:               resolutions.NewID(),
		SuggestionID:     req.SuggestionID,
		RunID:            run.ID,
		Repository:       absRepo,
		File:             req.File,
		CollisionKey:     req.CollisionKey,
		Revision:         rev,
		RegionID:         req.RegionID,
		Replacement:      req.Replacement,
		ContentHash:      analysis.ContentHash,
		ContextHash:      contextHash,
		Status:           initStatus,
		ApprovalStatus:   resolutions.ApprovalNone,
		ValidationStatus: resolutions.ValidationNotRun,
		CreatedAt:        time.Now().UTC(),
	}

	for _, reg := range analysis.ConflictRegions {
		if fmt.Sprintf("%d", reg.Index) == req.RegionID {
			res.StartLine = reg.StartLine
			res.EndLine = reg.EndLine
			res.Base = reg.Base
			res.Ours = reg.Ours
			res.Theirs = reg.Theirs
			break
		}
	}

	res = run.SaveResolution(res)

	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID:   res.ID,
		RunID:          res.RunID,
		Repository:     res.Repository,
		File:           res.File,
		Actor:          resolutions.ActorUser,
		Type:           resolutions.EventProposed,
		PreviousStatus: "",
		NewStatus:      res.Status,
	})

	writeJSON(w, http.StatusCreated, resolutionResponse{Success: true, Data: res})
}

// handleGetResolution returns the resolution by ID.
func handleGetResolution(w http.ResponseWriter, r *http.Request, run *runstate.Run, resID string) {
	res, ok := run.GetResolution(resID)
	if !ok {
		writeError(w, http.StatusNotFound, string(resolutions.CodeResolutionNotFound),
			fmt.Sprintf("resolution %s not found", resID))
		return
	}
	writeJSON(w, http.StatusOK, resolutionResponse{Success: true, Data: res})
}

// handleGetValidation returns the audit events and validation result for a resolution.
func handleGetValidation(w http.ResponseWriter, r *http.Request, run *runstate.Run, resID string) {
	res, ok := run.GetResolution(resID)
	if !ok {
		writeError(w, http.StatusNotFound, string(resolutions.CodeResolutionNotFound),
			fmt.Sprintf("resolution %s not found", resID))
		return
	}
	events := run.ResolutionEvents(resID)
	writeJSON(w, http.StatusOK, struct {
		Success            bool                          `json:"success"`
		Data               []resolutions.ResolutionEvent `json:"data"`
		ValidationStatus   string                        `json:"validationStatus,omitempty"`
		ValidationOutput   string                        `json:"validationOutput,omitempty"`
		ValidationExitCode int                           `json:"validationExitCode,omitempty"`
		ValidationDuration time.Duration                 `json:"validationDuration,omitempty"`
	}{
		Success:            true,
		Data:               events,
		ValidationStatus:   res.ValidationStatus,
		ValidationOutput:   res.ValidationOutput,
		ValidationExitCode: res.ValidationExitCode,
		ValidationDuration: res.ValidationDuration,
	})
}

// handlePreview generates a read-only diff for the resolution and marks it
// as "previewed". It never writes to the working-tree file.
func handlePreview(w http.ResponseWriter, r *http.Request, run *runstate.Run, resID string) {
	var req mutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body: "+err.Error())
		return
	}
	if req.ResolutionID == "" || req.ResolutionID != resID {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"resolutionId in request body is required and must match URL path")
		return
	}
	if req.RepositoryRoot == "" || req.File == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"repositoryRoot and file are required")
		return
	}

	res, ok := run.GetResolution(resID)
	if !ok {
		writeError(w, http.StatusNotFound, string(resolutions.CodeResolutionNotFound),
			fmt.Sprintf("resolution %s not found", resID))
		return
	}

	// Check status.
	if res.Status == resolutions.StatusApplied {
		writeResolutionError(w, &safeguard.SafeError{Code: resolutions.CodeAlreadyApplied, Message: "resolution is already applied"})
		return
	}
	if res.Status == resolutions.StatusStale {
		writeResolutionError(w, &safeguard.SafeError{Code: resolutions.CodeStaleFile, Message: "resolution is stale; re-analyze before previewing"})
		return
	}

	// Suggestion revision verification.
	if req.SuggestionRevision != 0 {
		if err := safeguard.VerifyRevision(res.Revision, req.SuggestionRevision); err != nil {
			writeResolutionError(w, err)
			return
		}
	}

	// Safety checks: canonicalize repo and verify file path.
	absRepo, err := safeguard.VerifyRepository(req.RepositoryRoot)
	if err != nil {
		writeResolutionError(w, err)
		return
	}
	if err := safeguard.VerifyRepositoryIdentity(res.Repository, absRepo); err != nil {
		writeResolutionError(w, err)
		return
	}
	absPath, err := safeguard.VerifyFilePath(absRepo, req.File)
	if err != nil {
		writeResolutionError(w, err)
		return
	}

	// Load the analysis snapshot.
	analysis, found := run.GetAnalysis(absRepo, req.File)
	if !found {
		analysis, found = run.FindAnalysis(req.File)
	}
	if !found {
		writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND",
			fmt.Sprintf("no analysis found for %s", req.File))
		return
	}

	// Working tree verification (content hash, conflict region, context hash).
	wtCheck, wtErr := safeguard.VerifyWorkingTree(absPath, analysis, res.RegionID)
	if wtErr != nil {
		writeResolutionError(w, wtErr)
		return
	}
	if req.ExpectedContentHash != "" && wtCheck.CurrentHash != req.ExpectedContentHash {
		writeResolutionError(w, &safeguard.SafeError{
			Code:    resolutions.CodeStaleFile,
			Message: fmt.Sprintf("expected content hash %s does not match working-tree hash %s", req.ExpectedContentHash, wtCheck.CurrentHash),
		})
		return
	}

	// Build the preview (read-only, never writes to disk).
	previewResult, buildErr := patch.Build(analysis, res, wtCheck.CurrentData)
	if buildErr != nil {
		writeResolutionError(w, buildErr)
		return
	}

	// Update the resolution.
	res.Status = resolutions.StatusPreviewed
	res.PreviewDiff = previewResult.UnifiedDiff
	res = run.SaveResolution(res)

	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID:   res.ID,
		RunID:          res.RunID,
		Repository:     res.Repository,
		File:           res.File,
		Actor:          resolutions.ActorUser,
		Type:           resolutions.EventPreviewed,
		PreviousStatus: resolutions.StatusProposed,
		NewStatus:      resolutions.StatusPreviewed,
	})

	writeJSON(w, http.StatusOK, resolutionResponse{Success: true, Data: res})
}

// handleApprove records explicit user approval for a previewed resolution.
func handleApprove(w http.ResponseWriter, r *http.Request, run *runstate.Run, resID string) {
	var req mutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body: "+err.Error())
		return
	}
	if req.ResolutionID == "" || req.ResolutionID != resID {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"resolutionId in request body is required and must match URL path")
		return
	}
	if req.RepositoryRoot == "" || req.File == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"repositoryRoot and file are required")
		return
	}

	res, ok := run.GetResolution(resID)
	if !ok {
		writeError(w, http.StatusNotFound, string(resolutions.CodeResolutionNotFound),
			fmt.Sprintf("resolution %s not found", resID))
		return
	}

	// Check status.
	if res.Status == resolutions.StatusApplied {
		writeResolutionError(w, &safeguard.SafeError{
			Code:    resolutions.CodeAlreadyApplied,
			Message: fmt.Sprintf("resolution %s is already applied", res.ID),
		})
		return
	}
	if res.Status == resolutions.StatusStale {
		writeResolutionError(w, &safeguard.SafeError{
			Code:    resolutions.CodeStaleFile,
			Message: fmt.Sprintf("resolution %s is stale; re-analyze before approving", res.ID),
		})
		return
	}

	// Suggestion revision verification.
	if req.SuggestionRevision != 0 {
		if err := safeguard.VerifyRevision(res.Revision, req.SuggestionRevision); err != nil {
			writeResolutionError(w, err)
			return
		}
	}

	// Safety: re-check repository and file.
	absRepo, err := safeguard.VerifyRepository(req.RepositoryRoot)
	if err != nil {
		writeResolutionError(w, err)
		return
	}
	if err := safeguard.VerifyRepositoryIdentity(res.Repository, absRepo); err != nil {
		writeResolutionError(w, err)
		return
	}
	absPath, err := safeguard.VerifyFilePath(absRepo, req.File)
	if err != nil {
		writeResolutionError(w, err)
		return
	}

	// Verify the working-tree file still matches stored context.
	analysis, found := run.GetAnalysis(absRepo, req.File)
	if !found {
		analysis, found = run.FindAnalysis(req.File)
	}
	if !found {
		writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND",
			fmt.Sprintf("no analysis found for %s", req.File))
		return
	}

	wtCheck, wtErr := safeguard.VerifyWorkingTree(absPath, analysis, res.RegionID)
	if wtErr != nil {
		writeResolutionError(w, wtErr)
		return
	}
	if req.ExpectedContentHash != "" && wtCheck.CurrentHash != req.ExpectedContentHash {
		writeResolutionError(w, &safeguard.SafeError{
			Code:    resolutions.CodeStaleFile,
			Message: fmt.Sprintf("expected content hash %s does not match working-tree hash %s", req.ExpectedContentHash, wtCheck.CurrentHash),
		})
		return
	}

	now := time.Now().UTC()
	prevStatus := res.Status
	res.ApprovalStatus = resolutions.ApprovalApproved
	res.Status = resolutions.StatusApproved
	res.ApprovedAt = now
	res = run.SaveResolution(res)

	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID:   res.ID,
		RunID:          res.RunID,
		Repository:     res.Repository,
		File:           res.File,
		Actor:          resolutions.ActorUser,
		Type:           resolutions.EventApproved,
		PreviousStatus: prevStatus,
		NewStatus:      resolutions.StatusApproved,
	})

	writeJSON(w, http.StatusOK, resolutionResponse{Success: true, Data: res})
}

// handleApply applies an approved resolution atomically.
func handleApply(w http.ResponseWriter, r *http.Request, run *runstate.Run, resID string) {
	var req mutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body: "+err.Error())
		return
	}
	if req.ResolutionID == "" || req.ResolutionID != resID {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"resolutionId in request body is required and must match URL path")
		return
	}
	if req.RepositoryRoot == "" || req.File == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"repositoryRoot and file are required")
		return
	}

	// Load the analysis snapshot before delegating to apply.Apply.
	absRepo, err := safeguard.VerifyRepository(req.RepositoryRoot)
	if err != nil {
		writeResolutionError(w, err)
		return
	}
	analysis, found := run.GetAnalysis(absRepo, req.File)
	if !found {
		analysis, found = run.FindAnalysis(req.File)
	}
	if !found {
		writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND",
			fmt.Sprintf("no analysis found for %s", req.File))
		return
	}

	valCfg := run.GetValidationConfig()
	if valCfg == nil {
		envCfg := validation.ConfigFromEnv()
		if len(envCfg.AllowList) > 0 {
			valCfg = &envCfg
		}
	}
	if valCfg != nil {
		if valCfg.RepoRoot == "" {
			valCfg.RepoRoot = absRepo
		}
		if valCfg.WorkDir == "" {
			valCfg.WorkDir = absRepo
		}
	}

	result, applyErr := apply.Apply(r.Context(), run, analysis, apply.ApplyRequest{
		ResolutionID:        resID,
		RepositoryRoot:      req.RepositoryRoot,
		File:                req.File,
		SuggestionRevision:  req.SuggestionRevision,
		ExpectedContentHash: req.ExpectedContentHash,
	}, valCfg)

	if applyErr != nil {
		writeResolutionError(w, applyErr)
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Success          bool                   `json:"success"`
		Data             resolutions.Resolution `json:"data"`
		PostApplyHash    string                 `json:"postApplyHash,omitempty"`
		ValidationStatus string                 `json:"validationStatus,omitempty"`
	}{
		Success:          true,
		Data:             result.Resolution,
		PostApplyHash:    result.PostApplyHash,
		ValidationStatus: result.Resolution.ValidationStatus,
	})
}

// handleRevert rolls back an applied resolution.
func handleRevert(w http.ResponseWriter, r *http.Request, run *runstate.Run, resID string) {
	var req struct {
		mutationRequest
		// PreApplyContent is optional; server-side snapshot stored on resolution is used when empty.
		PreApplyContent []byte `json:"preApplyContent,omitempty"`
		// PostApplyHash is optional; stored hash is used when empty.
		PostApplyHash string `json:"postApplyHash,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body: "+err.Error())
		return
	}
	if req.ResolutionID == "" || req.ResolutionID != resID {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"resolutionId in request body is required and must match URL path")
		return
	}
	if req.RepositoryRoot == "" || req.File == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"repositoryRoot and file are required")
		return
	}

	res, rollbackErr := apply.Rollback(r.Context(), run, apply.RollbackRequest{
		ResolutionID:    resID,
		RepositoryRoot:  req.RepositoryRoot,
		File:            req.File,
		PreApplyContent: req.PreApplyContent,
		PostApplyHash:   req.PostApplyHash,
	})

	if rollbackErr != nil {
		writeResolutionError(w, rollbackErr)
		return
	}
	writeJSON(w, http.StatusOK, resolutionResponse{Success: true, Data: res})
}

// handleAnalysisRefresh re-scans a repository/file pair and regenerates the
// analysis and suggestions after a stale-file failure.
func handleAnalysisRefresh(mux *http.ServeMux, run *runstate.Run) {
	mux.HandleFunc("/api/analysis/refresh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
				"POST /api/analysis/refresh")
			return
		}

		var req struct {
			RepositoryRoot string `json:"repositoryRoot"`
			File           string `json:"file"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		if req.RepositoryRoot == "" || req.File == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST",
				"repositoryRoot and file are required")
			return
		}

		// Mark all resolutions for this file as stale, capturing real previous status.
		absRepo, err := safeguard.VerifyRepository(req.RepositoryRoot)
		if err != nil {
			writeResolutionError(w, err)
			return
		}
		for _, res := range run.ResolutionsFor(absRepo, req.File) {
			if res.Status == resolutions.StatusProposed ||
				res.Status == resolutions.StatusPreviewed ||
				res.Status == resolutions.StatusApproved {
				prevStatus := res.Status
				res.Status = resolutions.StatusStale
				res.ApprovalStatus = resolutions.ApprovalExpired
				run.SaveResolution(res)
				run.RecordResolutionEvent(resolutions.ResolutionEvent{
					ResolutionID:   res.ID,
					RunID:          res.RunID,
					Repository:     res.Repository,
					File:           res.File,
					Actor:          resolutions.ActorSystem,
					Type:           resolutions.EventStale,
					PreviousStatus: prevStatus,
					NewStatus:      resolutions.StatusStale,
				})
			}
		}

		// Re-run real conflict analysis on the working-tree file using the engine pipeline.
		_ = engine.ProcessConflictFile(r.Context(), run, absRepo, req.File, engine.DefaultConfig(), false)

		// Regenerate suggestions and fresh resolutions for the file.
		generator := &suggestions.Generator{
			Cfg: ai.ConfigFromEnv(),
		}
		_, _ = generator.GenerateForFile(r.Context(), run, absRepo, req.File)

		writeJSON(w, http.StatusOK, struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{Success: true, Message: fmt.Sprintf("analysis refreshed for %s", req.File)})
	})
}

// writeResolutionError maps typed resolution and safeguard errors to stable
// HTTP error responses with appropriate status codes.
func writeResolutionError(w http.ResponseWriter, err error) {
	// Detect *safeguard.SafeError and *patch.PatchError.
	type coder interface {
		Error() string
	}
	switch e := err.(type) {
	case *safeguard.SafeError:
		status := resolutionHTTPStatus(e.Code)
		writeError(w, status, string(e.Code), e.Message)
	case *patch.PatchError:
		status := resolutionHTTPStatus(e.Code)
		writeError(w, status, string(e.Code), e.Message)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
	}
}

// resolutionHTTPStatus maps stable error codes to HTTP status codes.
func resolutionHTTPStatus(code resolutions.ErrorCode) int {
	switch code {
	case resolutions.CodeResolutionNotFound:
		return http.StatusNotFound
	case resolutions.CodeNotApproved, resolutions.CodeApprovalExpired:
		return http.StatusForbidden
	case resolutions.CodeAlreadyApplied:
		return http.StatusConflict
	case resolutions.CodeStaleFile:
		return http.StatusConflict
	case resolutions.CodePathEscape:
		return http.StatusBadRequest
	case resolutions.CodeRepositoryMismatch:
		return http.StatusBadRequest
	case resolutions.CodeInvalidPatch:
		return http.StatusUnprocessableEntity
	case resolutions.CodeValidationFailure:
		return http.StatusOK // applied but validation failed
	case resolutions.CodeRollbackFailure:
		return http.StatusConflict
	case resolutions.CodeApplyInProgress:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// resolutionHTTPStatus and writeResolutionError are complete above.

// _ prevents unused import errors.
var _ = context.Background
var _ = validation.StatusPassed
