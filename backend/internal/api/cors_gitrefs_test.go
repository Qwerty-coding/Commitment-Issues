package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"CommitIssues/internal/api"
	"CommitIssues/internal/runstate"
)

// TestAPI_CORSPreflight covers the regression this fix addresses: every API
// endpoint that accepts JSON POSTs must answer the browser's OPTIONS preflight
// with 204 and CORS headers, even when the request carries no body or query.
// Previously graph-inspect/prompt handlers returned 400 with no CORS headers,
// and the browser aborted the real request.
func TestAPI_CORSPreflight(t *testing.T) {
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	paths := []string{
		"/api/graph/focus",
		"/api/graph/expand",
		"/api/graph/collapse",
		"/api/prompt",
		"/api/compare",
		"/api/resolutions",
		"/api/analysis/refresh",
		"/api/git/branches",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, path, nil)
			req.Header.Set("Origin", "http://localhost:5173")
			req.Header.Set("Access-Control-Request-Method", "POST")
			req.Header.Set("Access-Control-Request-Headers", "content-type")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusNoContent {
				t.Fatalf("expected 204 for OPTIONS %s, got %d (body %q)", path, rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("missing Access-Control-Allow-Origin on %s: %q", path, got)
			}
			if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
				t.Errorf("missing Access-Control-Allow-Methods on %s", path)
			}
			if got := rr.Header().Get("Access-Control-Allow-Headers"); got == "" {
				t.Errorf("missing Access-Control-Allow-Headers on %s", path)
			}
		})
	}
}

// TestAPI_GitBranches_NonGitDir verifies the ref-listing endpoint degrades to
// an empty list for a non-git directory instead of a hard error, so the
// compare tab can still render.
func TestAPI_GitBranches_NonGitDir(t *testing.T) {
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/git/branches?repositoryRoot="+t.TempDir(), nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %q)", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); body == "" || body[0] != '{' {
		t.Fatalf("expected JSON body, got %q", body)
	}
}

// TestAPI_GitBranches_ListsRefs verifies branches, tags and remote refs are
// returned for a real repository.
func TestAPI_GitBranches_ListsRefs(t *testing.T) {
	repo := setupAPITestGitRepo(t)
	run := runstate.NewRun()
	handler := api.NewHandler(run, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/git/branches?repositoryRoot="+repo, nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %q)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"b1", "b2", "HEAD"} {
		if !contains(body, want) {
			t.Errorf("expected refs response to contain %q, got: %s", want, body)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
