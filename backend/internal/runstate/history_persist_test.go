package runstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistory_PersistOptIn_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	t.Setenv(EnvHistoryPersistPath, path)

	h := NewHistoryFromEnv(10)
	entry := RunHistory{
		RunID:         "run-1",
		Repository:    "/repo",
		StartedAt:     time.Unix(1000, 0).UTC(),
		CompletedAt:   time.Unix(2000, 0).UTC(),
		FilesAnalyzed: 3,
	}
	h.Record(entry)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("history file was not written: %v", err)
	}

	// A second store must load the persisted entry.
	reloaded := NewHistoryFromEnv(10)
	list := reloaded.List()
	if len(list) != 1 {
		t.Fatalf("reloaded %d entries, want 1", len(list))
	}
	if list[0].RunID != entry.RunID || list[0].FilesAnalyzed != entry.FilesAnalyzed {
		t.Errorf("reloaded entry mismatch: %#v", list[0])
	}
}

func TestHistory_PersistDisabled_NoFile(t *testing.T) {
	t.Setenv(EnvHistoryPersistPath, "")
	h := NewHistoryFromEnv(10)
	h.Record(RunHistory{RunID: "run-x", Repository: "/repo", StartedAt: time.Now().UTC()})
	if len(h.List()) != 1 {
		t.Fatal("in-memory history should still work when persistence is disabled")
	}
}

func TestHistory_PersistCorruptFileDegradesGracefully(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvHistoryPersistPath, path)

	h := NewHistoryFromEnv(10)
	if got := h.Len(); got != 0 {
		t.Fatalf("corrupt file should yield an empty history, got %d entries", got)
	}
	// It must remain usable and heal the file on the next write.
	h.Record(RunHistory{RunID: "run-heal", Repository: "/repo", StartedAt: time.Now().UTC()})
	if got := NewHistoryFromEnv(10).Len(); got != 1 {
		t.Fatalf("expected healed history to contain 1 entry, got %d", got)
	}
}
