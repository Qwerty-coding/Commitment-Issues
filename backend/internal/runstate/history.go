package runstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RunHistory is the read model for one analyzed repository inside one run.
// It is deterministic, isolated between runs and repositories, and carries
// no secrets.
type RunHistory struct {
	RunID      string    `json:"runId"`
	Repository string    `json:"repository"`
	StartedAt  time.Time `json:"startedAt"`
	// CompletedAt is zero while the run/repository is still in progress.
	CompletedAt time.Time `json:"completedAt,omitempty"`

	FilesAnalyzed  int      `json:"filesAnalyzed"`
	FailedFiles    int      `json:"failedFiles"`
	CollisionCount int      `json:"collisionCount"`
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	Succeeded      int      `json:"successfulSuggestions"`
	Failed         int      `json:"failedSuggestions"`
	BelowThreshold int      `json:"belowThresholdSuggestions"`
	TimedOut       bool     `json:"timedOut,omitempty"`
	Cancelled      bool     `json:"cancelled,omitempty"`
	ErrorSummaries []string `json:"errorSummaries,omitempty"`
}

// Key identifies a history entry by run + repository so updates are upserts.
func (h RunHistory) Key() string {
	return FileKey(h.RunID, h.Repository)
}

// History is a bounded in-memory run history. It is owned by the process
// component that creates it (the API server) rather than by any package
// global, so it introduces no package-level mutable state. Storage is
// Phase-2-scoped: database persistence is explicitly out of scope.
type History struct {
	mu      sync.Mutex
	entries map[string]RunHistory
	max     int
	// persistPath, when non-empty, is the opt-in JSON file the history is
	// loaded from and written to. Empty means purely in-memory.
	persistPath string
}

// DefaultHistoryLimit bounds the in-memory history.
const DefaultHistoryLimit = 100

// EnvHistoryPersistPath opts into durable history. When set to a file path,
// history is loaded on startup and persisted on every mutation. Unset means
// history stays in-memory only.
const EnvHistoryPersistPath = "HISTORY_PERSIST_PATH"

// NewHistory creates a bounded history store. A non-positive limit falls
// back to DefaultHistoryLimit.
func NewHistory(max int) *History {
	if max <= 0 {
		max = DefaultHistoryLimit
	}
	return &History{entries: make(map[string]RunHistory), max: max}
}

// NewHistoryFromEnv creates a history store, enabling durable history only when
// HISTORY_PERSIST_PATH is set (explicit opt-in). Existing entries are loaded
// from that file; a missing or corrupt file degrades gracefully to empty.
func NewHistoryFromEnv(max int) *History {
	return newPersistentHistory(max, strings.TrimSpace(os.Getenv(EnvHistoryPersistPath)))
}

// newPersistentHistory builds a bounded history that persists to path. An empty
// path yields an in-memory-only store.
func newPersistentHistory(max int, path string) *History {
	h := NewHistory(max)
	if path == "" {
		return h
	}
	h.persistPath = path

	data, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	var entries []RunHistory
	if err := json.Unmarshal(data, &entries); err != nil {
		return h
	}
	for _, e := range entries {
		if e.RunID == "" && e.Repository == "" {
			continue
		}
		h.entries[e.Key()] = e
	}
	h.evictLocked()
	return h
}

// persistLocked writes the current history to disk atomically. It must be
// called with h.mu held. Errors are intentionally swallowed: durable history is
// a best-effort convenience and must never fail a live run.
func (h *History) persistLocked() {
	if h.persistPath == "" {
		return
	}
	entries := make([]RunHistory, 0, len(h.entries))
	for _, e := range h.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key() < entries[j].Key() })

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	if dir := filepath.Dir(h.persistPath); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	tmp := h.persistPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, h.persistPath)
}

// evictLocked trims the store down to its bound. Must be called with h.mu held.
func (h *History) evictLocked() {
	if len(h.entries) <= h.max {
		return
	}
	victims := make([]RunHistory, 0, len(h.entries))
	for _, e := range h.entries {
		victims = append(victims, e)
	}
	sort.Slice(victims, func(i, j int) bool {
		ti, tj := evictTime(victims[i]), evictTime(victims[j])
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return victims[i].Key() < victims[j].Key()
	})
	for i := 0; i < len(h.entries)-h.max; i++ {
		delete(h.entries, victims[i].Key())
	}
}

// Record upserts a history entry. When the store is full, the entry with the
// oldest completion (falling back to start) time is evicted; the entry being
// upserted always survives its own eviction window.
func (h *History) Record(entry RunHistory) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[entry.Key()] = entry
	h.evictLocked()
	h.persistLocked()
}

func evictTime(e RunHistory) time.Time {
	if !e.CompletedAt.IsZero() {
		return e.CompletedAt
	}
	return e.StartedAt
}

// UpdateSuggestions merges suggestion counters into the entry for
// runID+repository. Missing entries are ignored (the caller records the base
// entry first), so suggestion generation never fabricates run history.
func (h *History) UpdateSuggestions(runID, repository string, succeeded, failed, belowThreshold int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.entries[FileKey(runID, repository)]
	if !ok {
		return
	}
	entry.Succeeded = succeeded
	entry.Failed = failed
	entry.BelowThreshold = belowThreshold
	h.entries[FileKey(runID, repository)] = entry
	h.persistLocked()
}

// Merge applies fn to the entry for runID+repository under the history lock
// (an atomic read-modify-write). It reports false when no base entry exists,
// in which case fn is not invoked and nothing is fabricated.
func (h *History) Merge(runID, repository string, fn func(*RunHistory)) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := FileKey(runID, repository)
	entry, ok := h.entries[key]
	if !ok {
		return false
	}
	fn(&entry)
	h.entries[key] = entry
	h.persistLocked()
	return true
}

// List returns every entry sorted deterministically: completion time (zero
// first, i.e. in-progress runs last), then run ID, then repository.
func (h *History) List() []RunHistory {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]RunHistory, 0, len(h.entries))
	for _, e := range h.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ca, cb := evictTime(a), evictTime(b)
		if !ca.Equal(cb) {
			return ca.Before(cb)
		}
		if a.RunID != b.RunID {
			return a.RunID < b.RunID
		}
		return a.Repository < b.Repository
	})
	return out
}

// Len returns the number of stored entries (test convenience).
func (h *History) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}
