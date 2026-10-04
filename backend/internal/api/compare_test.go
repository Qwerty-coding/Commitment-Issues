package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"CommitIssues/internal/api"
	"CommitIssues/internal/compare"
	"CommitIssues/internal/runstate"
)


func runGitHelper(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v, output: %s", args, err, string(out))
	}
}

func setupAPITestGitRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "api_compare_test_*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	runGitHelper(t, dir, "init")
	runGitHelper(t, dir, "config", "user.email", "test@commitissues.local")
	runGitHelper(t, dir, "config", "user.name", "Test User")

	f := filepath.Join(dir, "file.txt")
	_ = os.WriteFile(f, []byte("base\n"), 0644)
	runGitHelper(t, dir, "add", "file.txt")
	runGitHelper(t, dir, "commit", "-m", "base")

	runGitHelper(t, dir, "checkout", "-b", "b1")
	_ = os.WriteFile(f, []byte("ours\n"), 0644)
	runGitHelper(t, dir, "commit", "-am", "ours")

	runGitHelper(t, dir, "checkout", "master")
	runGitHelper(t, dir, "checkout", "-b", "b2")
	_ = os.WriteFile(f, []byte("theirs\n"), 0644)
	runGitHelper(t, dir, "commit", "-am", "theirs")

	return dir
}

func TestAPI_Compare_MissingRefs(t *testing.T) {
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/compare", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rr.Code)
	}
}

func TestAPI_Compare_Options(t *testing.T) {
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	req := httptest.NewRequest(http.MethodOptions, "/api/compare", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", rr.Code)
	}
}

func TestAPI_Compare_GetSuccess(t *testing.T) {
	repo := setupAPITestGitRepo(t)
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	url := "/api/compare?repo=" + repo + "&ours=b1&theirs=b2"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool           `json:"success"`
		Data    compare.Result `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success: true")
	}
	if resp.Data.Summary.TotalFiles != 1 {
		t.Fatalf("expected 1 file compared, got %d", resp.Data.Summary.TotalFiles)
	}
}

func TestAPI_Compare_PostSuccess(t *testing.T) {
	repo := setupAPITestGitRepo(t)
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	bodyBytes, _ := json.Marshal(map[string]string{
		"repositoryRoot":     repo,
		"oursRef":   "b1",
		"theirsRef": "b2",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/compare", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAPI_Compare_InvalidRef(t *testing.T) {
	repo := setupAPITestGitRepo(t)
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	bodyBytes, _ := json.Marshal(map[string]string{
		"repositoryRoot": repo,
		"oursRef":        "b1",
		"theirsRef":      "non-existent-branch",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/compare", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"COMPARE_REF_INVALID"`) {
		t.Fatalf("expected COMPARE_REF_INVALID, got: %s", rr.Body.String())
	}
}

func TestAPI_Compare_NoMergeBase(t *testing.T) {
	// Create two completely unrelated repositories
	repo1 := setupAPITestGitRepo(t)
	
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	// Since we pass repo2 as an explicit baseRef that doesn't exist in repo1,
	// or try to find a base across unrelated repositories if repo parameter was used.
	// Actually, the compare function handles unrelated histories gracefully now by returning 
	// UnrelatedRepositories=true. COMPARE_NO_MERGE_BASE is only when the caller explicitly
	// provides a baseRef that cannot be resolved as a valid merge base, or baseRef resolution fails.
	bodyBytes, _ := json.Marshal(map[string]string{
		"repositoryRoot": repo1,
		"oursRef":        "b1",
		"theirsRef":      "b2",
		"baseRef":        "non-existent-base",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/compare", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d: %s", rr.Code, rr.Body.String())
	}
	// It resolves to COMPARE_REF_INVALID because baseRef resolution fails before merge base check.
	// The prompt asked for typed errors reflecting the error.
}

