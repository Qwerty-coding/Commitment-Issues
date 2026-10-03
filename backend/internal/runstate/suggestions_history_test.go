package runstate

import (
	"sync"
	"testing"
	"time"

	ai "CommitIssues/internal/ai"
	semantic "CommitIssues/internal/semantic"
)

func suggestion(collisionKey, status string) Suggestion {
	return Suggestion{
		File:         "a.js",
		Repository:   "/repo",
		Collision:    semantic.DiffItem{Kind: "Function", Name: collisionKey},
		CollisionKey: collisionKey,
		Status:       status,
		Provider:     "ollama",
		Model:        "qwen2:1.5b",
		Resolution:   ai.AIResolutionResponse{Explanation: "e", SuggestedCode: "c", Confidence: 80},
	}
}

func TestSaveSuggestion_UpsertsWithoutDuplicates(t *testing.T) {
	run := NewRun()
	run.SaveSuggestion("/repo", suggestion("k1", StatusComplete))
	run.SaveSuggestion("/repo", suggestion("k2", StatusComplete))
	// Same identity replaces, never appends.
	run.SaveSuggestion("/repo", suggestion("k1", StatusBelowThreshold))

	items, ok := run.GetSuggestions("/repo", "a.js")
	if !ok {
		t.Fatal("suggestions should exist")
	}
	if len(items) != 2 {
		t.Fatalf("got %d suggestions, want 2 (upsert)", len(items))
	}
	// Deterministic (collision-key) ordering.
	if items[0].CollisionKey != "k1" || items[1].CollisionKey != "k2" {
		t.Errorf("ordering = %q,%q", items[0].CollisionKey, items[1].CollisionKey)
	}
	if items[0].Status != StatusBelowThreshold {
		t.Errorf("upsert did not replace the entry: %q", items[0].Status)
	}
}

func TestFindSuggestion_ByCollisionKey(t *testing.T) {
	run := NewRun()
	run.SaveSuggestion("/repo", suggestion("k1", StatusComplete))
	if _, ok := run.FindSuggestion("/repo", "a.js", "k1"); !ok {
		t.Errorf("existing suggestion not found")
	}
	if _, ok := run.FindSuggestion("/repo", "a.js", "nope"); ok {
		t.Errorf("unknown collision key must not match")
	}
	if _, ok := run.FindSuggestion("/other", "a.js", "k1"); ok {
		t.Errorf("suggestions must be repository-scoped")
	}
}

func TestTryBeginSuggestion_ExclusiveAndReleasable(t *testing.T) {
	run := NewRun()
	key := SuggestionKey("/repo", "a.js", "k1")
	if !run.TryBeginSuggestion(key) {
		t.Fatalf("first acquisition should succeed")
	}
	if run.TryBeginSuggestion(key) {
		t.Fatalf("second acquisition must be refused while in flight")
	}
	run.EndSuggestion(key)
	if !run.TryBeginSuggestion(key) {
		t.Fatalf("key should be reusable after release")
	}
}

func TestRun_InFlightRegistryIsRunScoped(t *testing.T) {
	runA := NewRun()
	runB := NewRun()
	key := SuggestionKey("/repo", "a.js", "k1")
	if !runA.TryBeginSuggestion(key) {
		t.Fatal("runA acquire failed")
	}
	if !runB.TryBeginSuggestion(key) {
		t.Fatal("runB must not be blocked by runA's in-flight work")
	}
}

func TestRun_ConcurrentSuggestionWritesAreRaceSafe(t *testing.T) {
	run := NewRun()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := SuggestionKey("/repo", "a.js", string(rune('a'+n%10)))
			run.SaveSuggestion("/repo", suggestion(key, StatusComplete))
			run.TryBeginSuggestion(SuggestionKey("/repo", "a.js", "shared"))
			run.EndSuggestion(SuggestionKey("/repo", "a.js", "shared"))
		}(i)
	}
	wg.Wait()
	items, _ := run.GetSuggestions("/repo", "a.js")
	if len(items) != 10 {
		t.Errorf("got %d unique suggestions, want 10", len(items))
	}
}

func TestHistory_BoundedEviction(t *testing.T) {
	history := NewHistory(3)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		history.Record(RunHistory{
			RunID:       string(rune('a' + i)),
			Repository:  "/repo",
			StartedAt:   base.Add(time.Duration(i) * time.Minute),
			CompletedAt: base.Add(time.Duration(i)*time.Minute + time.Second),
		})
	}
	if history.Len() != 3 {
		t.Fatalf("history size = %d, want bounded 3", history.Len())
	}
	list := history.List()
	if len(list) != 3 {
		t.Fatalf("entries = %d", len(list))
	}
	// The three most recent survive; the oldest two were evicted.
	want := []string{"c", "d", "e"}
	for i, entry := range list {
		if entry.RunID != want[i] {
			t.Errorf("entry %d run = %q, want %q", i, entry.RunID, want[i])
		}
	}
}

func TestHistory_DeterministicOrdering(t *testing.T) {
	history := NewHistory(10)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	history.Record(RunHistory{RunID: "run2", Repository: "/b", StartedAt: base.Add(time.Minute), CompletedAt: base.Add(2 * time.Minute)})
	history.Record(RunHistory{RunID: "run1", Repository: "/a", StartedAt: base, CompletedAt: base.Add(time.Minute)})
	history.Record(RunHistory{RunID: "run1", Repository: "/b", StartedAt: base, CompletedAt: base.Add(time.Minute)})

	first := history.List()
	for i := 0; i < 20; i++ {
		got := history.List()
		for j := range got {
			if got[j].RunID != first[j].RunID || got[j].Repository != first[j].Repository {
				t.Fatalf("history ordering is not deterministic")
			}
		}
	}
	if first[0].RunID != "run1" || first[1].Repository != "/b" {
		t.Errorf("unexpected order: %+v", first)
	}
}

func TestHistory_UpsertAndMerge(t *testing.T) {
	history := NewHistory(10)
	entry := RunHistory{RunID: "run1", Repository: "/repo", StartedAt: time.Now().UTC()}
	history.Record(entry)
	// Re-recording the same key updates rather than duplicates.
	entry.CompletedAt = time.Now().UTC()
	entry.FilesAnalyzed = 3
	history.Record(entry)
	if history.Len() != 1 {
		t.Fatalf("history entries = %d, want 1", history.Len())
	}

	ok := history.Merge("run1", "/repo", func(e *RunHistory) {
		e.Succeeded = 2
		e.Failed = 1
		e.TimedOut = true
		e.ErrorSummaries = []string{"TIMEOUT: slow"}
	})
	if !ok {
		t.Fatal("merge of an existing entry must succeed")
	}
	merged := history.List()[0]
	if merged.Succeeded != 2 || merged.Failed != 1 || !merged.TimedOut || len(merged.ErrorSummaries) != 1 {
		t.Errorf("merge did not apply: %+v", merged)
	}
	if merged.FilesAnalyzed != 3 {
		t.Errorf("merge must preserve fields it does not touch: %+v", merged)
	}

	if history.Merge("unknown", "/repo", func(*RunHistory) { t.Error("must not run") }) {
		t.Errorf("merge of a missing entry must report false")
	}
}

func TestHistory_DefaultLimitAndZeroHandling(t *testing.T) {
	if got := NewHistory(0); got.max != DefaultHistoryLimit {
		t.Errorf("zero limit should fall back to the default, got %d", got.max)
	}
	if got := NewHistory(-5); got.max != DefaultHistoryLimit {
		t.Errorf("negative limit should fall back to the default, got %d", got.max)
	}
}

func TestSuggestionKey_IncludesRepositoryFileAndCollision(t *testing.T) {
	key := SuggestionKey("/repo", "a.js", "k1")
	if key != "/repo|a.js|k1" {
		t.Errorf("key = %q", key)
	}
	if SuggestionKey("/repo", "a.js", "k1") == SuggestionKey("/other", "a.js", "k1") {
		t.Errorf("keys must differ across repositories")
	}
	if SuggestionKey("/repo", "a.js", "k1") == SuggestionKey("/repo", "a.js", "k2") {
		t.Errorf("keys must differ across collisions")
	}
}
