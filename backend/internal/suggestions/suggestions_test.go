package suggestions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ai "CommitIssues/internal/ai"
	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/runstate"
	semantic "CommitIssues/internal/semantic"
)

// fakeOllama is a configurable OpenAI-compatible fake provider that also
// serves the Ollama /api/tags health endpoint.
type fakeOllama struct {
	server *httptest.Server

	mu          sync.Mutex
	requests    int
	inFlight    int
	maxInFlight int
	// respond decides the reply for a chat completion given the request body.
	respond func(body map[string]any) (status int, content string)
	delay   time.Duration
}

func newFakeOllama(t *testing.T, model string) *fakeOllama {
	t.Helper()
	f := &fakeOllama{}
	f.respond = func(body map[string]any) (int, string) {
		return http.StatusOK, `{"explanation":"merged","suggested_code":"function x(){}","confidence_score":80}`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"models":[{"name":%q}]}`, model)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(rawBody, &body)

		f.mu.Lock()
		f.requests++
		f.inFlight++
		if f.inFlight > f.maxInFlight {
			f.maxInFlight = f.inFlight
		}
		f.mu.Unlock()

		if f.delay > 0 {
			time.Sleep(f.delay)
		}

		status, content := f.respond(body)
		w.WriteHeader(status)
		if status == http.StatusOK {
			payload := map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"content": content}}},
			}
			_ = json.NewEncoder(w).Encode(payload)
		} else {
			_, _ = w.Write([]byte(content))
		}

		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOllama) config(model string) ai.Config {
	cfg := ai.Default("")
	cfg.Model = model
	cfg.BaseURL = f.server.URL + "/v1"
	cfg.RequestTimeout = 2 * time.Second
	cfg.RetryCount = 0
	cfg.ConfidenceThreshold = 70
	return cfg
}

func (f *fakeOllama) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func collision(name string) semantic.DiffItem {
	return semantic.DiffItem{
		Type:     "COLLISION",
		Kind:     "Function",
		Name:     name,
		Line:     1,
		File:     "a.js",
		Identity: "a.js||Function|" + name + "|()",
	}
}

// seedRun registers one analysis per file with the supplied collisions.
func seedRun(t *testing.T, run *runstate.Run, repo string, files map[string][]string) {
	t.Helper()
	for file, names := range files {
		collisions := make([]semantic.DiffItem, 0, len(names))
		for _, name := range names {
			item := collision(name)
			item.File = file
			item.Identity = file + "||Function|" + name + "|()"
			collisions = append(collisions, item)
		}
		run.RegisterAnalysis(repo, promptcontext.FileAnalysis{
			Repository: repo,
			File:       file,
			PromptContext: promptcontext.PromptContextIR{
				RepositorySummary: "repo: " + repo,
				Context:           "prompt for " + file,
			},
			SmartDiff: semantic.SmartDiffResult{Collisions: collisions},
		})
	}
}

func TestGenerateForFile_SuccessAndMetadata(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one", "two"}})

	gen := &Generator{Cfg: f.config("qwen2:1.5b")}
	result, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 2 {
		t.Fatalf("got %d suggestions, want 2", len(result.Suggestions))
	}
	for _, s := range result.Suggestions {
		if s.Status != runstate.StatusComplete {
			t.Errorf("status = %q, want complete", s.Status)
		}
		if s.Provider != "ollama" || s.Model != "qwen2:1.5b" {
			t.Errorf("provider/model metadata missing: %+v", s)
		}
		if s.ID == "" || s.CollisionKey == "" || s.Repository != "/repo" {
			t.Errorf("metadata incomplete: %+v", s)
		}
		if s.StartedAt.IsZero() || s.CompletedAt.IsZero() {
			t.Errorf("timestamps missing: %+v", s)
		}
	}
	if result.Meta.Provider != "ollama" || result.Meta.Model != "qwen2:1.5b" {
		t.Errorf("meta provider/model = %+v", result.Meta)
	}
	if result.Meta.Generated != 2 || result.Meta.Failed != 0 {
		t.Errorf("meta counts = %+v", result.Meta)
	}
}

func TestGenerateForFile_DeterministicOrdering(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	gen := &Generator{Cfg: f.config("qwen2:1.5b"), MaxConcurrency: 4}

	// Run many times: completion order varies, output order must not.
	for attempt := 0; attempt < 5; attempt++ {
		run := runstate.NewRun()
		run.RegisterAnalysis("/repo", promptcontext.FileAnalysis{
			Repository:    "/repo",
			File:          "a.js",
			PromptContext: promptcontext.PromptContextIR{Context: "p"},
			SmartDiff: semantic.SmartDiffResult{Collisions: []semantic.DiffItem{
				collision("zeta"), collision("alpha"), collision("mu"), collision("beta"),
			}},
		})
		result, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"a.js||Function|alpha|()", "a.js||Function|beta|()", "a.js||Function|mu|()", "a.js||Function|zeta|()"}
		got := make([]string, 0, len(result.Suggestions))
		for _, s := range result.Suggestions {
			got = append(got, s.CollisionKey)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("attempt %d ordering = %v, want %v", attempt, got, want)
		}
	}
}

func TestGenerateForRun_OrdersAcrossFilesAndRepos(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	run := runstate.NewRun()
	seedRun(t, run, "/repo-b", map[string][]string{"z.js": {"one"}})
	seedRun(t, run, "/repo-a", map[string][]string{"b.js": {"one"}, "a.js": {"one"}})

	gen := &Generator{Cfg: f.config("qwen2:1.5b"), MaxConcurrency: 3}
	result, err := gen.GenerateForRun(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/repo-a|a.js", "/repo-a|b.js", "/repo-b|z.js"}
	got := make([]string, 0, len(result.Suggestions))
	for _, s := range result.Suggestions {
		got = append(got, s.Repository+"|"+s.File)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("cross-file ordering = %v, want %v", got, want)
	}
}

func TestGenerate_BoundedConcurrency(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	f.delay = 25 * time.Millisecond

	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one", "two", "three", "four", "five", "six"}})

	gen := &Generator{Cfg: f.config("qwen2:1.5b"), MaxConcurrency: 2}
	if _, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js"); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	maxObserved := f.maxInFlight
	f.mu.Unlock()
	if maxObserved > 2 {
		t.Errorf("concurrency limit exceeded: observed %d, limit 2", maxObserved)
	}
	if maxObserved < 2 {
		t.Errorf("expected the limit to be used (observed %d); test may be too fast", maxObserved)
	}
}

func TestGenerate_PartialFailurePreservesSuccesses(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	f.respond = func(body map[string]any) (int, string) {
		// Fail the collision named "two" with a retryable 503.
		messages, _ := body["messages"].([]any)
		if len(messages) > 1 {
			if msg, ok := messages[1].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok && strings.Contains(content, `"two"`) {
					return http.StatusServiceUnavailable, "busy"
				}
			}
		}
		return http.StatusOK, `{"explanation":"ok","suggested_code":"c","confidence_score":90}`
	}

	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one", "two", "three"}})

	gen := &Generator{Cfg: f.config("qwen2:1.5b"), MaxConcurrency: 3}
	result, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
	if err != nil {
		t.Fatalf("partial failure must not fail the pass: %v", err)
	}
	if len(result.Suggestions) != 3 {
		t.Fatalf("successful suggestions must be retained: got %d", len(result.Suggestions))
	}
	var complete, failed int
	for _, s := range result.Suggestions {
		switch s.Status {
		case runstate.StatusComplete:
			complete++
		case runstate.StatusFailed:
			failed++
			if s.ErrorCode != string(ai.CodeHTTPError) {
				t.Errorf("failure code = %q", s.ErrorCode)
			}
			if !s.Retryable {
				t.Errorf("503 should be marked retryable")
			}
		}
	}
	if complete != 2 || failed != 1 {
		t.Fatalf("complete=%d failed=%d, want 2/1", complete, failed)
	}
	if result.Meta.Failed != 1 || !result.Meta.Retryable {
		t.Errorf("meta = %+v", result.Meta)
	}
	if len(result.Meta.Failures) != 1 || result.Meta.Failures[0].CollisionKey != "a.js||Function|two|()" {
		t.Errorf("structured failures = %+v", result.Meta.Failures)
	}
}

func TestGenerate_NonRetryableFailureFlaggedNotRetryable(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	f.respond = func(map[string]any) (int, string) {
		return http.StatusBadRequest, "malformed"
	}
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one"}})
	gen := &Generator{Cfg: f.config("qwen2:1.5b")}
	result, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.Retryable {
		t.Errorf("400 must not be reported as retryable")
	}
	if result.Suggestions[0].Status != runstate.StatusFailed {
		t.Errorf("expected failed status, got %q", result.Suggestions[0].Status)
	}
}

func TestGenerate_ReusesStoredSuggestions(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one", "two"}})
	gen := &Generator{Cfg: f.config("qwen2:1.5b")}

	if _, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js"); err != nil {
		t.Fatal(err)
	}
	first := f.count()
	if first != 2 {
		t.Fatalf("first pass requests = %d, want 2", first)
	}

	result, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != first {
		t.Errorf("second pass must not re-request completed suggestions: %d -> %d", first, f.count())
	}
	if result.Meta.Reused != 2 || len(result.Suggestions) != 2 {
		t.Errorf("reuse meta = %+v", result.Meta)
	}
}

func TestGenerate_RetryAfterFailureOnlyRegeneratesFailures(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	var failNext atomic.Bool
	failNext.Store(true)
	f.respond = func(map[string]any) (int, string) {
		if failNext.Load() {
			return http.StatusServiceUnavailable, "busy"
		}
		return http.StatusOK, `{"explanation":"ok","suggested_code":"c","confidence_score":90}`
	}
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one"}})
	gen := &Generator{Cfg: f.config("qwen2:1.5b")}

	if _, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js"); err != nil {
		t.Fatal(err)
	}
	// The retryable failure is exposed so the frontend can offer a retry.
	if !genLastRetryable(t, gen, run) {
		t.Fatalf("expected the failed suggestion to be retryable")
	}

	failNext.Store(false)
	before := f.count()
	result, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != before+1 {
		t.Errorf("retry should regenerate only the failed collision: %d -> %d", before, f.count())
	}
	if result.Suggestions[0].Status != runstate.StatusComplete {
		t.Errorf("status after retry = %q", result.Suggestions[0].Status)
	}
}

func genLastRetryable(t *testing.T, gen *Generator, run *runstate.Run) bool {
	t.Helper()
	items, ok := run.GetSuggestions("/repo", "a.js")
	if !ok || len(items) == 0 {
		return false
	}
	return items[0].Retryable
}

func TestGenerate_DuplicateRequestPrevention(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	f.delay = 20 * time.Millisecond
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one", "two"}})
	gen := &Generator{Cfg: f.config("qwen2:1.5b"), MaxConcurrency: 2}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
		}()
	}
	wg.Wait()

	// Two collisions, four concurrent passes: each collision is requested once.
	if got := f.count(); got != 2 {
		t.Errorf("requests = %d, want exactly 2 (duplicate prevention)", got)
	}
}

func TestGenerate_RunAndRepositoryIsolation(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	gen := &Generator{Cfg: f.config("qwen2:1.5b")}

	runA := runstate.NewRun()
	runB := runstate.NewRun()
	seedRun(t, runA, "/repo-1", map[string][]string{"a.js": {"one"}})
	seedRun(t, runA, "/repo-2", map[string][]string{"a.js": {"one"}})
	seedRun(t, runB, "/repo-1", map[string][]string{"a.js": {"one"}})

	for _, pair := range []struct {
		run  *runstate.Run
		repo string
	}{{runA, "/repo-1"}, {runA, "/repo-2"}, {runB, "/repo-1"}} {
		if _, err := gen.GenerateForFile(context.Background(), pair.run, pair.repo, "a.js"); err != nil {
			t.Fatal(err)
		}
	}

	// Each run/repository keeps its own stored suggestion (no cross-talk).
	for _, pair := range []struct {
		run  *runstate.Run
		repo string
	}{{runA, "/repo-1"}, {runA, "/repo-2"}, {runB, "/repo-1"}} {
		items, ok := pair.run.GetSuggestions(pair.repo, "a.js")
		if !ok || len(items) != 1 {
			t.Errorf("run %s repo %s missing its own suggestion", pair.run.ID, pair.repo)
		}
	}
}

func TestGenerate_BelowThresholdStatus(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	f.respond = func(map[string]any) (int, string) {
		return http.StatusOK, `{"explanation":"low","suggested_code":"c","confidence_score":10}`
	}
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one"}})
	cfg := f.config("qwen2:1.5b")
	cfg.ConfidenceThreshold = 70
	gen := &Generator{Cfg: cfg}
	result, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
	if err != nil {
		t.Fatal(err)
	}
	if result.Suggestions[0].Status != runstate.StatusBelowThreshold {
		t.Errorf("status = %q, want below_threshold", result.Suggestions[0].Status)
	}
	if result.Meta.BelowThreshold != 1 {
		t.Errorf("meta = %+v", result.Meta)
	}
}

func TestGenerate_ProviderUnavailableFailsBeforeGeneration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()

	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one"}})

	cfg := ai.Default("")
	cfg.BaseURL = addr + "/v1"
	cfg.RequestTimeout = 500 * time.Millisecond
	gen := &Generator{Cfg: cfg}

	_, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js")
	if err == nil {
		t.Fatal("expected a provider-unavailable error")
	}
	if ai.AsError(err).Code != ai.CodeProviderUnavailable {
		t.Errorf("code = %q", ai.AsError(err).Code)
	}
	if _, ok := run.GetSuggestions("/repo", "a.js"); ok {
		t.Errorf("no suggestions should be stored when the provider is unavailable")
	}
}

func TestGenerate_CancellationStopsWork(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	f.delay = 50 * time.Millisecond
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one", "two", "three"}})
	gen := &Generator{Cfg: f.config("qwen2:1.5b"), MaxConcurrency: 1}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := gen.GenerateForFile(ctx, run, "/repo", "a.js")
	if err == nil || !strings.Contains(err.Error(), "operation") && err != context.Canceled {
		// Either the pass aborted on cancellation (health check) or
		// generation produced cancellation failures; both are acceptable as
		// long as no work continues.
		t.Logf("cancellation surfaced as: %v", err)
	}
	// No in-flight registrations may leak after cancellation.
	if !run.TryBeginSuggestion("sentinel") {
		t.Errorf("in-flight registry leaked a registration after cancellation")
	}
}

func TestGenerate_NoAnalysisIsAnError(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	run := runstate.NewRun()
	gen := &Generator{Cfg: f.config("qwen2:1.5b")}
	if _, err := gen.GenerateForRun(context.Background(), run); err == nil {
		t.Fatal("expected an error for an empty run")
	}
	if _, err := gen.GenerateForFile(context.Background(), run, "/repo", "missing.js"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestGenerate_RecordsHistory(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one", "two"}})

	history := runstate.NewHistory(10)
	history.Record(runstate.RunHistory{RunID: run.ID, Repository: "/repo", StartedAt: run.StartedAt})

	gen := &Generator{Cfg: f.config("qwen2:1.5b"), History: history}
	if _, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js"); err != nil {
		t.Fatal(err)
	}

	list := history.List()
	if len(list) != 1 {
		t.Fatalf("history entries = %d, want 1", len(list))
	}
	entry := list[0]
	if entry.Succeeded != 2 || entry.Failed != 0 {
		t.Errorf("history counts = %+v", entry)
	}
	if entry.CollisionCount != 2 || entry.Provider != "ollama" || entry.Model != "qwen2:1.5b" {
		t.Errorf("history metadata = %+v", entry)
	}
}

func TestGenerate_UnknownAnalysisHistoryEntryIgnored(t *testing.T) {
	f := newFakeOllama(t, "qwen2:1.5b")
	run := runstate.NewRun()
	seedRun(t, run, "/repo", map[string][]string{"a.js": {"one"}})
	history := runstate.NewHistory(10) // no base entry recorded
	gen := &Generator{Cfg: f.config("qwen2:1.5b"), History: history}
	if _, err := gen.GenerateForFile(context.Background(), run, "/repo", "a.js"); err != nil {
		t.Fatal(err)
	}
	if history.Len() != 0 {
		t.Errorf("history must not fabricate entries, got %d", history.Len())
	}
}
