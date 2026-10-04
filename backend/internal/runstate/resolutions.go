package runstate

import (
	"sort"
	"time"

	"CommitIssues/internal/resolutions"
	"CommitIssues/internal/validation"
)

// Resolution storage on the Run. Resolutions are keyed by their
// opaque, URL-safe ID and carry their own repository
// qualification (repository root + relative file path), so
// identical paths in different repositories never overwrite one
// another. Events are an append-only, sequence-ordered audit
// log. All access is guarded by the run lock, mirroring the
// existing collection behaviour.

// SaveResolution stores or replaces one resolution, keyed by its
// opaque ID. When the resolution carries no ID, one is
// generated and returned in the stored copy. The stored value is
// returned so callers always persist the generated identifier.
func (r *Run) SaveResolution(res resolutions.Resolution) resolutions.Resolution {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res.ID == "" {
		res.ID = resolutions.NewID()
	}
	r.resolutions[res.ID] = res
	return res
}

// GetResolution returns the resolution with the given opaque ID.
func (r *Run) GetResolution(id string) (resolutions.Resolution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res, ok := r.resolutions[id]
	if !ok {
		return resolutions.Resolution{}, false
	}
	return res, true
}

// FindResolutionBySuggestion returns the most recent resolution
// for a suggestion ID (deterministic tie-break by ID), so a
// client can locate the resolution belonging to the current
// suggestion revision.
func (r *Run) FindResolutionBySuggestion(suggestionID string) (resolutions.Resolution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best resolutions.Resolution
	found := false
	for _, key := range sortedKeys(r.resolutions) {
		res := r.resolutions[key]
		if res.SuggestionID != suggestionID {
			continue
		}
		if !found || res.CreatedAt.After(best.CreatedAt) ||
			(res.CreatedAt.Equal(best.CreatedAt) && res.ID > best.ID) {
			best = res
			found = true
		}
	}
	return best, found
}

// ResolutionsFor returns every resolution for a repository/file
// pair, ordered by creation time then ID.
func (r *Run) ResolutionsFor(repoRoot, file string) []resolutions.Resolution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]resolutions.Resolution, 0, len(r.resolutions))
	for _, key := range sortedKeys(r.resolutions) {
		res := r.resolutions[key]
		if res.Repository == repoRoot && res.File == file {
			out = append(out, res)
		}
	}
	sortResolutionByCreated(out)
	return out
}

// AllResolutions returns every resolution ordered deterministically
// by repository, file, creation time and ID, so enumeration is
// stable regardless of insertion order.
func (r *Run) AllResolutions() []resolutions.Resolution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]resolutions.Resolution, 0, len(r.resolutions))
	for _, key := range sortedKeys(r.resolutions) {
		out = append(out, r.resolutions[key])
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Repository != b.Repository {
			return a.Repository < b.Repository
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return out
}

func sortResolutionByCreated(items []resolutions.Resolution) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
}

// RecordResolutionEvent appends one immutable audit event,
// assigning the next run-wide monotonic sequence number under
// the run lock. A zero timestamp is replaced with the current
// UTC time so the audit order is always well-defined.
func (r *Run) RecordResolutionEvent(event resolutions.ResolutionEvent) resolutions.ResolutionEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	r.eventSeq++
	event.Sequence = r.eventSeq
	r.resolutionEvents = append(r.resolutionEvents, event)
	return event
}

// ResolutionEvents returns the audit events of one resolution in
// sequence order.
func (r *Run) ResolutionEvents(resolutionID string) []resolutions.ResolutionEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]resolutions.ResolutionEvent, 0, len(r.resolutionEvents))
	for _, event := range r.resolutionEvents {
		if event.ResolutionID == resolutionID {
			out = append(out, event)
		}
	}
	return out
}

// AllResolutionEvents returns every audit event in sequence
// order, as a copy that callers may mutate freely.
func (r *Run) AllResolutionEvents() []resolutions.ResolutionEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]resolutions.ResolutionEvent, len(r.resolutionEvents))
	copy(out, r.resolutionEvents)
	return out
}

// TryBeginResolutionApply atomically registers an in-flight
// apply for the given resolution ID. It reports false when an
// apply for the same resolution is already in progress, which
// makes concurrent apply attempts for one resolution produce
// exactly one success and one APPLY_IN_PROGRESS (or, after the
// first completes, ALREADY_APPLIED) response. Always pair with
// EndResolutionApply.
func (r *Run) TryBeginResolutionApply(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.resolutionInFlight[id]; busy {
		return false
	}
	r.resolutionInFlight[id] = struct{}{}
	return true
}

// EndResolutionApply releases an in-flight apply registration.
func (r *Run) EndResolutionApply(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.resolutionInFlight, id)
}

// TryBeginResolutionRevert registers an in-flight revert for the resolution ID,
// reusing the per-resolution in-flight tracker.
func (r *Run) TryBeginResolutionRevert(id string) bool {
	return r.TryBeginResolutionApply(id)
}

// EndResolutionRevert releases the in-flight revert registration.
func (r *Run) EndResolutionRevert(id string) {
	r.EndResolutionApply(id)
}

// SetValidationConfig stores the validation configuration for this run.
func (r *Run) SetValidationConfig(cfg validation.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validationCfg = &cfg
}

// GetValidationConfig returns the stored validation configuration, if any.
func (r *Run) GetValidationConfig() *validation.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.validationCfg == nil {
		return nil
	}
	cp := *r.validationCfg
	return &cp
}
