package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/runstate"
)

// fakeProvider serves both the Ollama health endpoint and OpenAI-compatible
// chat completions for engine-level AI tests.
func fakeProvider(t *testing.T, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = fmt.Fprint(w, `{"models":[{"name":"qwen2:1.5b"}]}`)
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"content": content}}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func aiConfig(baseURL string) ai.Config {
	cfg := ai.Default("")
	cfg.Model = "qwen2:1.5b"
	cfg.BaseURL = baseURL
	cfg.RequestTimeout = 2 * time.Second
	cfg.RetryCount = 0
	return cfg
}

func TestProcessConflictFile_RecordsAISuggestions(t *testing.T) {
	srv := fakeProvider(t, `{"explanation":"merged both","suggested_code":"function x(){}","confidence_score":85}`)
	dir := t.TempDir()
	writeConflictFile(t, dir, "ai.js", "const x = 1;", "const x = 2;")

	run := runstate.NewRun()
	cfg := validConfig()
	cfg.AI = aiConfig(srv.URL + "/v1")

	outcome := ProcessConflictFile(context.Background(), run, dir, "ai.js", cfg, true)
	if outcome.Err != nil {
		t.Fatalf("AI resolution should not fail the file: %v", outcome.Err)
	}

	items, ok := run.GetSuggestions(dir, "ai.js")
	if !ok || len(items) == 0 {
		t.Fatalf("expected run-scoped suggestions, got %v", items)
	}
	for _, item := range items {
		if item.Status != runstate.StatusComplete {
			t.Errorf("status = %q, want complete (%s)", item.Status, item.ErrorMessage)
		}
		if item.Provider != "ollama" || item.Model != "qwen2:1.5b" {
			t.Errorf("provider/model metadata = %+v", item)
		}
		if item.Resolution.SuggestedCode == "" || item.Resolution.Explanation == "" {
			t.Errorf("resolution not recorded: %+v", item)
		}
		if item.Repository != dir || item.CollisionKey == "" {
			t.Errorf("repository/collision metadata = %+v", item)
		}
	}
}

func TestProcessConflictFile_ProviderUnavailableDoesNotFailFile(t *testing.T) {
	// Reserve and close a port so the endpoint is unreachable.
	tmp := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := tmp.URL
	tmp.Close()

	dir := t.TempDir()
	writeConflictFile(t, dir, "ai.js", "const x = 1;", "const x = 2;")

	run := runstate.NewRun()
	cfg := validConfig()
	cfg.AI = aiConfig(addr + "/v1")

	outcome := ProcessConflictFile(context.Background(), run, dir, "ai.js", cfg, true)
	if outcome.Err != nil {
		t.Fatalf("an unavailable provider must not fail the analysis file: %v", outcome.Err)
	}
	items, ok := run.GetSuggestions(dir, "ai.js")
	if !ok || len(items) == 0 {
		t.Fatal("provider setup failure should be recorded for each collision")
	}
	for _, item := range items {
		if item.Status != runstate.StatusFailed || item.ErrorCode != string(ai.CodeProviderUnavailable) {
			t.Errorf("provider failure metadata = %+v", item)
		}
	}
	// The deterministic analysis artifacts are still registered.
	if _, ok := run.GetAnalysis(dir, "ai.js"); !ok {
		t.Errorf("analysis must still be registered")
	}
}

func TestProcessConflictFile_InvalidSchemaRecordsFailedSuggestion(t *testing.T) {
	srv := fakeProvider(t, `{"explanation":"","suggested_code":"","confidence_score":0}`)
	dir := t.TempDir()
	writeConflictFile(t, dir, "ai.js", "const x = 1;", "const x = 2;")

	run := runstate.NewRun()
	cfg := validConfig()
	cfg.AI = aiConfig(srv.URL + "/v1")
	cfg.AI.RetryCount = 1 // schema failures must not be retried

	outcome := ProcessConflictFile(context.Background(), run, dir, "ai.js", cfg, true)
	if outcome.Err != nil {
		t.Fatalf("a schema failure must not fail the file: %v", outcome.Err)
	}
	items, _ := run.GetSuggestions(dir, "ai.js")
	if len(items) == 0 {
		t.Fatal("expected a recorded failure")
	}
	if items[0].Status != runstate.StatusFailed {
		t.Errorf("status = %q, want failed", items[0].Status)
	}
	if items[0].ErrorCode != string(ai.CodeSchemaInvalid) {
		t.Errorf("error code = %q, want %q", items[0].ErrorCode, ai.CodeSchemaInvalid)
	}
	if items[0].Retryable {
		t.Errorf("schema failures must not be retryable")
	}
}
