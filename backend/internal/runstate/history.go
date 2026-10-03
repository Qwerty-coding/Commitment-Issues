package runstate

import (
	"sort"
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
}

// DefaultHistoryLimit bounds the in-memory history.
const DefaultHistoryLimit = 100

// NewHistory creates a bounded history store. A non-positive limit falls
// back to DefaultHistoryLimit.
func NewHistory(max int) *History {
	if max <= 0 {
		max = DefaultHistoryLimit
	}
	return &History{entries: make(map[string]RunHistory), max: max}
}

// Record upserts a history entry. When the store is full, the entry with the
// oldest completion (falling back to start) time is evicted; the entry being
// upserted always survives its own eviction window.
func (h *History) Record(entry RunHistory) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[entry.Key()] = entry

	if len(h.entries) <= h.max {
		return
	}
	// Evict the oldest entry (deterministic tie-break by key).
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
