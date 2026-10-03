package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/runstate"
	semantic "CommitIssues/internal/semantic"
)

// ─── Fake provider ────────────────────────────────────────────────────────────

func fakeOllama(t *testing.T, respond func() (int, string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"models":[{"name":"qwen2:1.5b"}]}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		status, content := respond()
		w.WriteHeader(status)
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"content": content}}},
			})
			return
		}
		_, _ = w.Write([]byte(content))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func useOllamaEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("AI_PROVIDER", "ollama")
	t.Setenv("AI_MODEL", "qwen2:1.5b")
	t.Setenv("AI_BASE_URL", baseURL)
	t.Setenv("AI_TIMEOUT", "2s")
	t.Setenv("AI_RETRIES", "0")
}

func seededRun(t *testing.T, repo, file string, collisionNames []string) *runstate.Run {
	t.Helper()
	run := runstate.NewRun()
	collisions := make([]semantic.DiffItem, 0, len(collisionNames))
	for _, name := range collisionNames {
		collisions = append(collisions, semantic.DiffItem{
			Type: "COLLISION", Kind: "Function", Name: name, Line: 1, File: file,
			Identity: file + "||Function|" + name + "|()",
		})
	}
	run.RegisterAnalysis(repo, promptcontext.FileAnalysis{
		Repository:    repo,
		File:          file,
		PromptContext: promptcontext.PromptContextIR{RepositorySummary: repo, Context: "prompt"},
		SmartDiff:     semantic.SmartDiffResult{Collisions: collisions},
	})
	return run
}

type suggestionsPayload struct {
	Success bool             `json:"success"`
	Data    []SuggestionItem `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Meta *struct {
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		RunID          string `json:"runId"`
		Generated      int    `json:"generated"`
		Reused         int    `json:"reused"`
		Failed         int    `json:"failed"`
		BelowThreshold int    `json:"belowThreshold"`
		Retryable      bool   `json:"retryable"`
		Failures       []struct {
			File         string `json:"file"`
			CollisionKey string `json:"collisionKey"`
			Code         string `json:"code"`
			Retryable    bool   `json:"retryable"`
		} `json:"failures"`
	} `json:"meta"`
}

func doGet(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// ─── /api/suggestions ─────────────────────────────────────────────────────────

func TestSuggestions_SuccessWithMetadata(t *testing.T) {
	srv := fakeOllama(t, func() (int, string) {
		return http.StatusOK, `{"explanation":"merged","suggested_code":"c","confidence_score":88}`
	})
	useOllamaEnv(t, srv.URL+"/v1")

	run := seededRun(t, "/repo", "a.js", []string{"one", "two"})
	handler := NewHandler(run, runstate.NewHistory(10))

	rec := doGet(t, handler, "/api/suggestions?file=a.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload suggestionsPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || len(payload.Data) != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	for _, item := range payload.Data {
		if item.Provider != "ollama" || item.Model != "qwen2:1.5b" || item.Status != runstate.StatusComplete {
			t.Errorf("item metadata = %+v", item)
		}
		if item.Resolution.Explanation == "" || item.Resolution.SuggestedCode == "" {
			t.Errorf("resolution missing: %+v", item)
		}
	}
	if payload.Meta == nil || payload.Meta.Provider != "ollama" || payload.Meta.Generated != 2 {
		t.Errorf("meta = %+v", payload.Meta)
	}
}

func TestSuggestions_ProviderUnavailable(t *testing.T) {
	// Bind then close so the endpoint is unreachable.
	tmp := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := tmp.URL
	tmp.Close()
	useOllamaEnv(t, addr+"/v1")

	run := seededRun(t, "/repo", "a.js", []string{"one"})
	handler := NewHandler(run, runstate.NewHistory(10))

	rec := doGet(t, handler, "/api/suggestions?file=a.js")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload suggestionsPayload
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload.Success || payload.Error == nil || payload.Error.Code != "PROVIDER_UNAVAILABLE" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestSuggestions_NoAnalysisIs404(t *testing.T) {
	srv := fakeOllama(t, func() (int, string) {
		return http.StatusOK, `{"explanation":"e","suggested_code":"c","confidence_score":50}`
	})
	useOllamaEnv(t, srv.URL+"/v1")
	handler := NewHandler(runstate.NewRun(), runstate.NewHistory(10))

	rec := doGet(t, handler, "/api/suggestions")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSuggestions_PartialFailureKeepsSuccesses(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = fmt.Fprint(w, `{"models":[{"name":"qwen2:1.5b"}]}`)
			return
		}
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("busy"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"content": `{"explanation":"ok","suggested_code":"c","confidence_score":90}`,
			}}},
		})
	}))
	defer srv.Close()
	useOllamaEnv(t, srv.URL+"/v1")

	run := seededRun(t, "/repo", "a.js", []string{"one", "two"})
	handler := NewHandler(run, runstate.NewHistory(10))

	rec := doGet(t, handler, "/api/suggestions?file=a.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("partial failure must still return 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload suggestionsPayload
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if len(payload.Data) != 2 {
		t.Fatalf("data = %+v", payload.Data)
	}
	if payload.Meta == nil || payload.Meta.Failed != 1 || payload.Meta.Generated != 1 {
		t.Fatalf("meta = %+v", payload.Meta)
	}
	if !payload.Meta.Retryable || len(payload.Meta.Failures) != 1 {
		t.Errorf("retryable failure metadata missing: %+v", payload.Meta)
	}
	// The successful suggestion retains its full resolution.
	var complete, failed int
	for _, item := range payload.Data {
		switch item.Status {
		case runstate.StatusComplete:
			complete++
		case runstate.StatusFailed:
			failed++
			if item.ErrorCode != "HTTP_ERROR" {
				t.Errorf("error code = %q", item.ErrorCode)
			}
		}
	}
	if complete != 1 || failed != 1 {
		t.Errorf("complete=%d failed=%d", complete, failed)
	}
}

func TestSuggestions_TimeoutIsPartialFailureNotServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = fmt.Fprint(w, `{"models":[{"name":"qwen2:1.5b"}]}`)
			return
		}
		time.Sleep(300 * time.Millisecond) // exceeds the 50ms AI_TIMEOUT set below
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"content": `{"explanation":"late","suggested_code":"c","confidence_score":90}`,
			}}},
		})
	}))
	defer srv.Close()
	t.Setenv("AI_PROVIDER", "ollama")
	t.Setenv("AI_MODEL", "qwen2:1.5b")
	t.Setenv("AI_BASE_URL", srv.URL+"/v1")
	t.Setenv("AI_TIMEOUT", "50ms")
	t.Setenv("AI_RETRIES", "0")

	run := seededRun(t, "/repo", "a.js", []string{"one"})
	handler := NewHandler(run, runstate.NewHistory(10))

	rec := doGet(t, handler, "/api/suggestions?file=a.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("per-request timeout should be a partial failure, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload suggestionsPayload
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if len(payload.Data) != 1 || payload.Data[0].Status != runstate.StatusFailed {
		t.Fatalf("data = %+v", payload.Data)
	}
	if payload.Data[0].ErrorCode != "TIMEOUT" {
		t.Errorf("error code = %q, want TIMEOUT", payload.Data[0].ErrorCode)
	}
	if payload.Data[0].Retryable {
		t.Errorf("timeouts must not be retryable")
	}
}

func TestSuggestions_APIKeyNotLeakedInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = fmt.Fprint(w, `{"models":[{"name":"qwen2:1.5b"}]}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, "invalid key: LEAKEDSECRET")
	}))
	defer srv.Close()
	t.Setenv("AI_PROVIDER", "ollama")
	t.Setenv("AI_MODEL", "qwen2:1.5b")
	t.Setenv("AI_BASE_URL", srv.URL+"/v1")
	t.Setenv("AI_API_KEY", "LEAKEDSECRET")
	t.Setenv("AI_TIMEOUT", "2s")
	t.Setenv("AI_RETRIES", "0")

	run := seededRun(t, "/repo", "a.js", []string{"one"})
	handler := NewHandler(run, runstate.NewHistory(10))
	rec := doGet(t, handler, "/api/suggestions?file=a.js")

	if strings.Contains(rec.Body.String(), "LEAKEDSECRET") {
		t.Errorf("response leaked the API key: %s", rec.Body.String())
	}
}

// ─── /api/history ─────────────────────────────────────────────────────────────

func TestHistory_EmptyState(t *testing.T) {
	handler := NewHandler(runstate.NewRun(), runstate.NewHistory(10))
	rec := doGet(t, handler, "/api/history")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload struct {
		Success bool                  `json:"success"`
		Data    []runstate.RunHistory `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data == nil || len(payload.Data) != 0 {
		t.Errorf("payload = %+v", payload)
	}
}

func TestHistory_ReturnsRecordedRuns(t *testing.T) {
	history := runstate.NewHistory(10)
	history.Record(runstate.RunHistory{
		RunID: "run1", Repository: "/repo", StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(),
		FilesAnalyzed: 2, CollisionCount: 3, Provider: "ollama", Model: "qwen2:1.5b", Succeeded: 3,
	})
	handler := NewHandler(runstate.NewRun(), history)
	rec := doGet(t, handler, "/api/history")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload struct {
		Success bool                  `json:"success"`
		Data    []runstate.RunHistory `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].RunID != "run1" || payload.Data[0].Succeeded != 3 {
		t.Errorf("payload = %+v", payload)
	}
}

func TestNewHandler_DefaultsNilRunAndHistory(t *testing.T) {
	handler := NewHandler(nil, nil)
	if rec := doGet(t, handler, "/api/history"); rec.Code != http.StatusOK {
		t.Errorf("nil history should not break /api/history: %d", rec.Code)
	}
	if rec := doGet(t, handler, "/api/analysis"); rec.Code != http.StatusOK {
		t.Errorf("nil run should not break /api/analysis: %d", rec.Code)
	}
}
