package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
	"CommitIssues/internal/resolutions"
	"CommitIssues/internal/runstate"
)

func setupTestServerWithConflict(t *testing.T, filename, content string) (string, *runstate.Run, http.Handler, promptcontext.FileAnalysis) {
	t.Helper()
	repo := t.TempDir()
	fullPath := filepath.Join(repo, filename)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	allLines := strings.Split(content, "\n")
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}

	parsed := git.ParseConflictRegions(content)
	ctxHashes := make(map[string]string)
	for _, r := range parsed.Regions {
		key := fmt.Sprintf("%d", r.Index)
		ctxHashes[key] = fileutil.RegionContextHash(allLines, r)
	}

	analysis := promptcontext.FileAnalysis{
		Repository:          repo,
		File:                filename,
		ContentHash:         fileutil.HashContent([]byte(content)),
		ConflictRegions:     parsed.Regions,
		RegionContextHashes: ctxHashes,
	}

	run := runstate.NewRun()
	run.RegisterAnalysis(repo, analysis)
	handler := NewHandler(run, nil)

	return repo, run, handler, analysis
}

func doPostJSON(t *testing.T, handler http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func parseErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var res struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode error body: %v (body: %s)", err, rec.Body.String())
	}
	return res.Error.Code, res.Error.Message
}

func TestAPI_GetResolution(t *testing.T) {
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, run, handler, _ := setupTestServerWithConflict(t, "file.js", content)

	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	// 1. Success
	rec := doGet(t, handler, "/api/resolutions/"+res.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool                   `json:"success"`
		Data    resolutions.Resolution `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.ID != res.ID {
		t.Errorf("expected ID %s, got %s", res.ID, resp.Data.ID)
	}

	// 2. Not found
	recNotFound := doGet(t, handler, "/api/resolutions/non-existent-id")
	if recNotFound.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", recNotFound.Code)
	}
	code, _ := parseErrorResponse(t, recNotFound)
	if code != string(resolutions.CodeResolutionNotFound) {
		t.Errorf("expected RESOLUTION_NOT_FOUND, got %s", code)
	}
}

func TestAPI_FullWorkflow_PreviewApproveApplyRevert(t *testing.T) {
	content := "// top\n<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n// bottom\n"
	repo, run, handler, _ := setupTestServerWithConflict(t, "file.js", content)

	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	// ── 1. PREVIEW ────────────────────────────────────────────────────────
	recPrev := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/preview", map[string]any{
		"resolutionId":       res.ID,
		"repositoryRoot":     repo,
		"file":               "file.js",
		"suggestionRevision": 1,
	})
	if recPrev.Code != http.StatusOK {
		t.Fatalf("preview failed: %d: %s", recPrev.Code, recPrev.Body.String())
	}
	var prevResp struct {
		Success bool                   `json:"success"`
		Data    resolutions.Resolution `json:"data"`
	}
	_ = json.NewDecoder(recPrev.Body).Decode(&prevResp)
	if prevResp.Data.Status != resolutions.StatusPreviewed {
		t.Errorf("expected status previewed, got %s", prevResp.Data.Status)
	}
	if !strings.Contains(prevResp.Data.PreviewDiff, "+fn_resolved();") {
		t.Errorf("preview diff missing replacement: %s", prevResp.Data.PreviewDiff)
	}

	// Try Apply BEFORE approval — must be rejected!
	recEarlyApply := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/apply", map[string]any{
		"resolutionId":       res.ID,
		"repositoryRoot":     repo,
		"file":               "file.js",
		"suggestionRevision": 1,
	})
	if recEarlyApply.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for unapproved apply, got %d", recEarlyApply.Code)
	}
	code, _ := parseErrorResponse(t, recEarlyApply)
	if code != string(resolutions.CodeNotApproved) {
		t.Errorf("expected NOT_APPROVED, got %s", code)
	}

	// ── 2. APPROVE ────────────────────────────────────────────────────────
	recApprove := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/approve", map[string]any{
		"resolutionId":       res.ID,
		"repositoryRoot":     repo,
		"file":               "file.js",
		"suggestionRevision": 1,
	})
	if recApprove.Code != http.StatusOK {
		t.Fatalf("approve failed: %d: %s", recApprove.Code, recApprove.Body.String())
	}
	var appResp struct {
		Success bool                   `json:"success"`
		Data    resolutions.Resolution `json:"data"`
	}
	_ = json.NewDecoder(recApprove.Body).Decode(&appResp)
	if appResp.Data.Status != resolutions.StatusApproved || appResp.Data.ApprovalStatus != resolutions.ApprovalApproved {
		t.Errorf("expected status approved, got %s / %s", appResp.Data.Status, appResp.Data.ApprovalStatus)
	}

	// ── 3. APPLY ──────────────────────────────────────────────────────────
	recApply := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/apply", map[string]any{
		"resolutionId":       res.ID,
		"repositoryRoot":     repo,
		"file":               "file.js",
		"suggestionRevision": 1,
	})
	if recApply.Code != http.StatusOK {
		t.Fatalf("apply failed: %d: %s", recApply.Code, recApply.Body.String())
	}
	var applyResp struct {
		Success       bool                   `json:"success"`
		Data          resolutions.Resolution `json:"data"`
		PostApplyHash string                 `json:"postApplyHash"`
	}
	_ = json.NewDecoder(recApply.Body).Decode(&applyResp)
	if applyResp.Data.Status != resolutions.StatusApplied {
		t.Errorf("expected status applied, got %s", applyResp.Data.Status)
	}

	// Verify file modified on disk
	diskBytes, _ := os.ReadFile(filepath.Join(repo, "file.js"))
	if !strings.Contains(string(diskBytes), "fn_resolved();") {
		t.Errorf("file on disk not updated: %s", string(diskBytes))
	}

	// ── 4. VALIDATION ENDPOINT ───────────────────────────────────────────
	recVal := doGet(t, handler, "/api/resolutions/"+res.ID+"/validation")
	if recVal.Code != http.StatusOK {
		t.Fatalf("validation endpoint failed: %d", recVal.Code)
	}

	// ── 5. REVERT ─────────────────────────────────────────────────────────
	recRevert := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/revert", map[string]any{
		"resolutionId":    res.ID,
		"repositoryRoot":  repo,
		"file":            "file.js",
		"postApplyHash":   applyResp.PostApplyHash,
	})
	if recRevert.Code != http.StatusOK {
		t.Fatalf("revert failed: %d: %s", recRevert.Code, recRevert.Body.String())
	}
	var revResp struct {
		Success bool                   `json:"success"`
		Data    resolutions.Resolution `json:"data"`
	}
	_ = json.NewDecoder(recRevert.Body).Decode(&revResp)
	if revResp.Data.Status != resolutions.StatusReverted {
		t.Errorf("expected status reverted, got %s", revResp.Data.Status)
	}

	// Verify disk file restored
	diskRestored, _ := os.ReadFile(filepath.Join(repo, "file.js"))
	if string(diskRestored) != content {
		t.Errorf("disk file not restored:\ngot: %s\nwant: %s", string(diskRestored), content)
	}
}

func TestAPI_MutationSafetyValidationErrors(t *testing.T) {
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, run, handler, _ := setupTestServerWithConflict(t, "file.js", content)

	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	// 1. Missing resolution ID in body
	recNoID := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/preview", map[string]any{
		"repositoryRoot": repo,
		"file":           "file.js",
	})
	if recNoID.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing resolutionId, got %d", recNoID.Code)
	}

	// 2. Path traversal
	recEscape := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/preview", map[string]any{
		"resolutionId":       res.ID,
		"repositoryRoot":     repo,
		"file":               "../outside.js",
		"suggestionRevision": 1,
	})
	if recEscape.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d", recEscape.Code)
	}
	code, _ := parseErrorResponse(t, recEscape)
	if code != string(resolutions.CodePathEscape) {
		t.Errorf("expected PATH_ESCAPE, got %s", code)
	}

	// 3. Repository mismatch
	otherRepo := t.TempDir()
	recRepoMismatch := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/preview", map[string]any{
		"resolutionId":       res.ID,
		"repositoryRoot":     otherRepo,
		"file":               "file.js",
		"suggestionRevision": 1,
	})
	if recRepoMismatch.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for repo mismatch, got %d", recRepoMismatch.Code)
	}
	code, _ = parseErrorResponse(t, recRepoMismatch)
	if code != string(resolutions.CodeRepositoryMismatch) {
		t.Errorf("expected REPOSITORY_MISMATCH, got %s", code)
	}

	// 4. Stale file: file changed on disk
	diskPath := filepath.Join(repo, "file.js")
	_ = os.WriteFile(diskPath, []byte(content+"// edited\n"), 0o644)
	recStale := doPostJSON(t, handler, "/api/resolutions/"+res.ID+"/preview", map[string]any{
		"resolutionId":       res.ID,
		"repositoryRoot":     repo,
		"file":               "file.js",
		"suggestionRevision": 1,
	})
	if recStale.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for stale file, got %d", recStale.Code)
	}
	code, _ = parseErrorResponse(t, recStale)
	if code != string(resolutions.CodeStaleFile) {
		t.Errorf("expected STALE_FILE, got %s", code)
	}
}

func TestAPI_AnalysisRefresh(t *testing.T) {
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, run, handler, _ := setupTestServerWithConflict(t, "file.js", content)

	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	// Modify file
	diskPath := filepath.Join(repo, "file.js")
	newContent := "<<<<<<< ours\nfn_ours_updated();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	_ = os.WriteFile(diskPath, []byte(newContent), 0o644)

	// Trigger analysis refresh
	recRefresh := doPostJSON(t, handler, "/api/analysis/refresh", map[string]any{
		"repositoryRoot": repo,
		"file":           "file.js",
	})
	if recRefresh.Code != http.StatusOK {
		t.Fatalf("refresh failed: %d: %s", recRefresh.Code, recRefresh.Body.String())
	}

	// Verify resolution was marked stale
	storedRes, _ := run.GetResolution(res.ID)
	if storedRes.Status != resolutions.StatusStale || storedRes.ApprovalStatus != resolutions.ApprovalExpired {
		t.Errorf("expected resolution to become stale, got status=%s approval=%s", storedRes.Status, storedRes.ApprovalStatus)
	}

	// Verify analysis hash updated
	updatedAnalysis, _ := run.GetAnalysis(repo, "file.js")
	if updatedAnalysis.ContentHash != fileutil.HashContent([]byte(newContent)) {
		t.Errorf("analysis hash not updated: %s", updatedAnalysis.ContentHash)
	}
}
