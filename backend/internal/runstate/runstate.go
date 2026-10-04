// Package runstate holds all mutable state produced by a single analysis run.
//
// Previously analysis, prompt context, graph and report data lived in global
// package-level variables, which leaked between runs and duplicated graph
// objects on repeated scans. A Run is now created per execution, owns an
// isolated set of maps, and returns every collection in a deterministic,
// sorted order.
//
// Every collection is keyed by a repository-safe key (repository root + "|" +
// relative file path) so that identical relative paths in different
// repositories never overwrite one another.
package runstate

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/cache"
	promptcontext "CommitIssues/internal/context"
	graph "CommitIssues/internal/graph"
	"CommitIssues/internal/resolutions"
	semantic "CommitIssues/internal/semantic"
	"CommitIssues/internal/validation"
)

// FileKey builds the repository-safe key used for every per-file collection.
// When no repository is known (legacy callers/tests) it degrades to the plain
// file path.
func FileKey(repoRoot, file string) string {
	if repoRoot == "" {
		return file
	}
	return repoRoot + "|" + file
}

// Suggestion is a run-scoped AI suggestion for a single collision. It is
// stored on the Run rather than in a package-level cache so suggestions can
// never leak across runs or repositories.
//
// Phase 2 added metadata additively (ID, Repository, CollisionKey, Provider,
// Model, Status, ErrorCode, Retryable, timestamps); the original fields and
// their JSON names are unchanged for backward compatibility.
type Suggestion struct {
	File       string                  `json:"file"`
	Collision  semantic.DiffItem       `json:"collision"`
	Resolution ai.AIResolutionResponse `json:"resolution"`

	// ── Phase 2 additive metadata ────────────────────────────────────────
	// ID is a stable suggestion identifier: <runID>/<repoKey>/<collisionKey>.
	ID string `json:"id,omitempty"`
	// Repository is the canonical repository root the suggestion belongs to.
	Repository string `json:"repository,omitempty"`
	// CollisionKey is the stable per-collision identity (DiffKey), used for
	// dedupe and lookups independent of line numbers.
	CollisionKey string `json:"collisionKey,omitempty"`
	// Provider and Model record exactly which configuration produced the
	// suggestion (visible in logs and response metadata).
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Revision is the generation of the suggestion. It is
	// incremented when the suggestion is regenerated for the
	// same collision, which invalidates resolutions (and
	// their approvals) built from earlier revisions.
	// Phase 2 semantics; populated by the regeneration path.
	Revision int `json:"revision,omitempty"`
	// RegionID identifies the exact conflict region within the file
	// that this suggestion was generated for.
	RegionID string `json:"regionId,omitempty"`
	// ResolutionID links this suggestion to its active resolution.
	ResolutionID string `json:"resolutionId,omitempty"`
	// Status is one of "complete", "below_threshold", "failed".
	Status string `json:"status,omitempty"`
	// ErrorCode is the typed failure code when Status is "failed".
	ErrorCode string `json:"errorCode,omitempty"`
	// Retryable reports whether the API classified the failure as transient,
	// so the frontend can offer a retry.
	Retryable bool `json:"retryable,omitempty"`
	// ErrorMessage carries a redacted, human-readable failure summary.
	ErrorMessage string `json:"errorMessage,omitempty"`
	// StartedAt/CompletedAt bound the generation attempt (UTC).
	StartedAt   time.Time `json:"startedAt,omitempty"`
	CompletedAt time.Time `json:"completedAt,omitempty"`
}

// Suggestion status values.
const (
	// StatusComplete marks a successfully validated suggestion.
	StatusComplete = "complete"
	// StatusBelowThreshold marks a valid suggestion whose confidence is under
	// the configured threshold; it is still returned to the caller.
	StatusBelowThreshold = "below_threshold"
	// StatusFailed marks a suggestion whose generation failed.
	StatusFailed = "failed"
	// StatusManualReview marks a suggestion whose conflict region cannot be mapped
	// or requires manual review.
	StatusManualReview = "manual_review"
)

// SuggestionKey builds the run-unique lookup key for one suggestion:
// repository root + file + stable collision identity.
func SuggestionKey(repoRoot, file, collisionKey string) string {
	return FileKey(repoRoot, file) + "|" + collisionKey
}

// RepositoryMetadata describes the repository a run is analysing.
type RepositoryMetadata struct {
	Name           string `json:"name"`
	CurrentBranch  string `json:"currentBranch"`
	IncomingBranch string `json:"incomingBranch"`
}

// Run owns the isolated state for one analysis run.
type Run struct {
	ID        string
	StartedAt time.Time

	mu                 sync.RWMutex
	analyses           map[string]promptcontext.FileAnalysis
	prompts            map[string]promptcontext.PromptContextIR
	graphs             map[string]graph.CyGraph
	reports            map[string][]byte
	suggestions        map[string][]Suggestion
	inFlight           map[string]struct{}
	resolutions        map[string]resolutions.Resolution
	resolutionEvents   []resolutions.ResolutionEvent
	resolutionInFlight map[string]struct{}
	eventSeq           int64
	repositoryMD       RepositoryMetadata
	validationCfg      *validation.Config
	// readmes stores the per-repository README excerpt keyed by repoRoot.
	// Isolation matches the analyses map: identical repo roots in different
	// runs never share state.
	readmes map[string]string
	// cacheStats holds the AST cache statistics captured after processing so
	// the API can surface them. Nil until SetCacheStats is called.
	cacheStats *cache.Stats
}

// NewRun creates a run with a unique identifier.
func NewRun() *Run {
	return NewRunWithID(newRunID())
}

// NewRunWithID creates a run with a caller-supplied identifier. An empty ID is
// replaced with a generated one.
func NewRunWithID(id string) *Run {
	if id == "" {
		id = newRunID()
	}
	return &Run{
		ID:                 id,
		StartedAt:          time.Now().UTC(),
		analyses:           make(map[string]promptcontext.FileAnalysis),
		prompts:            make(map[string]promptcontext.PromptContextIR),
		graphs:             make(map[string]graph.CyGraph),
		reports:            make(map[string][]byte),
		suggestions:        make(map[string][]Suggestion),
		inFlight:           make(map[string]struct{}),
		resolutions:        make(map[string]resolutions.Resolution),
		resolutionEvents:   make([]resolutions.ResolutionEvent, 0),
		resolutionInFlight: make(map[string]struct{}),
		readmes:            make(map[string]string),
	}

}

func newRunID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(buf)
}

// sortedKeys returns the map keys in ascending order so every collection can be
// enumerated deterministically regardless of Go's map iteration order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// matchesFile reports whether a repository-safe key refers to the given file.
func matchesFile(key, file string) bool {
	return key == file || strings.HasSuffix(key, "|"+file)
}

// ─── Repository metadata ─────────────────────────────────────────────────────

// SetRepositoryMetadata stores the metadata for this run.
func (r *Run) SetRepositoryMetadata(meta RepositoryMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.repositoryMD = meta
}

// GetRepositoryMetadata returns the metadata for this run.
func (r *Run) GetRepositoryMetadata() RepositoryMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.repositoryMD
}

// ─── README excerpts ──────────────────────────────────────────────────────────

// RegisterReadme stores the README excerpt for the given repository root.
// Only the repo-root README is stored; subdirectory READMEs are not tracked.
// Calling this multiple times for the same repoRoot is idempotent — the first
// write wins so ProcessRepository can call it unconditionally before the worker
// pool without races.
func (r *Run) RegisterReadme(repoRoot, excerpt string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.readmes[repoRoot]; !exists {
		r.readmes[repoRoot] = excerpt
	}
}

// Readme returns the stored README excerpt for a repository root and whether
// one was registered. An empty excerpt is valid (README absent or disabled).
func (r *Run) Readme(repoRoot string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.readmes[repoRoot]
	return v, ok
}

// ─── Cache statistics ───────────────────────────────────────────────────────

// SetCacheStats records the AST cache statistics for this run.
func (r *Run) SetCacheStats(stats cache.Stats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyStats := stats
	r.cacheStats = &copyStats
}

// GetCacheStats returns the recorded AST cache statistics, or nil when none
// were captured.
func (r *Run) GetCacheStats() *cache.Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cacheStats == nil {
		return nil
	}
	copyStats := *r.cacheStats
	return &copyStats
}



// ─── Analyses ────────────────────────────────────────────────────────────────

// RegisterAnalysis stores the analysis for a repository/file pair, replacing
// any prior entry so repeated scans do not duplicate data.
func (r *Run) RegisterAnalysis(repoRoot string, analysis promptcontext.FileAnalysis) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.analyses[FileKey(repoRoot, analysis.File)] = analysis
}

// GetAnalysis returns the analysis for a repository/file pair.
func (r *Run) GetAnalysis(repoRoot, file string) (promptcontext.FileAnalysis, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	analysis, ok := r.analyses[FileKey(repoRoot, file)]
	return analysis, ok
}

// FindAnalysis returns the first analysis (deterministically ordered) whose
// file matches, regardless of repository. It exists for the frontend's
// file-only lookups; use GetAnalysis when the repository is known.
func (r *Run) FindAnalysis(file string) (promptcontext.FileAnalysis, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, key := range sortedKeys(r.analyses) {
		if matchesFile(key, file) {
			return r.analyses[key], true
		}
	}
	return promptcontext.FileAnalysis{}, false
}

// AllAnalyses returns every analysis sorted by repository-safe key.
func (r *Run) AllAnalyses() []promptcontext.FileAnalysis {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := sortedKeys(r.analyses)
	out := make([]promptcontext.FileAnalysis, 0, len(keys))
	for _, key := range keys {
		out = append(out, r.analyses[key])
	}
	return out
}

// ─── Prompt contexts ─────────────────────────────────────────────────────────

// RegisterPromptContext stores the prompt context for a repository/file pair.
func (r *Run) RegisterPromptContext(repoRoot, fileName string, ctx promptcontext.PromptContextIR) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts[FileKey(repoRoot, fileName)] = ctx
}

// GetPromptContext returns the prompt context for a repository/file pair.
func (r *Run) GetPromptContext(repoRoot, fileName string) (promptcontext.PromptContextIR, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ctx, ok := r.prompts[FileKey(repoRoot, fileName)]
	return ctx, ok
}

// FindPromptContext returns the first prompt context (deterministically
// ordered) whose file matches, regardless of repository.
func (r *Run) FindPromptContext(fileName string) (promptcontext.PromptContextIR, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, key := range sortedKeys(r.prompts) {
		if matchesFile(key, fileName) {
			return r.prompts[key], true
		}
	}
	return promptcontext.PromptContextIR{}, false
}

// AllPromptContexts returns every prompt context sorted by repository-safe key.
func (r *Run) AllPromptContexts() []promptcontext.PromptContextIR {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := sortedKeys(r.prompts)
	out := make([]promptcontext.PromptContextIR, 0, len(keys))
	for _, key := range keys {
		out = append(out, r.prompts[key])
	}
	return out
}

// ─── Graphs ──────────────────────────────────────────────────────────────────

// RegisterGraph stores the graph for a repository/file pair, replacing any
// prior graph for that pair so repeated scans never duplicate nodes.
func (r *Run) RegisterGraph(repoRoot, file string, g graph.CyGraph) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.graphs[FileKey(repoRoot, file)] = g
}

// MergedGraph returns the deterministic, deduplicated union of every graph.
func (r *Run) MergedGraph() graph.CyGraph {
	r.mu.RLock()
	keys := sortedKeys(r.graphs)
	graphs := make([]graph.CyGraph, 0, len(keys))
	for _, key := range keys {
		graphs = append(graphs, r.graphs[key])
	}
	r.mu.RUnlock()
	return graph.MergeGraphs(graphs)
}

// MergedGraphDTO returns the merged graph flattened for the API.
func (r *Run) MergedGraphDTO() graph.GraphDTO {
	return r.MergedGraph().ToDTO()
}

// GraphKeys returns the sorted repository-safe keys of registered graphs.
func (r *Run) GraphKeys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sortedKeys(r.graphs)
}

// ─── Reports ─────────────────────────────────────────────────────────────────

// SaveReport stores report bytes for a repository/file pair.
func (r *Run) SaveReport(repoRoot, fileName string, jsonBytes []byte) string {
	key := FileKey(repoRoot, fileName)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports[key] = append([]byte(nil), jsonBytes...)
	return key
}

// GetReport retrieves report bytes for a repository/file pair.
func (r *Run) GetReport(repoRoot, fileName string) ([]byte, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.reports[FileKey(repoRoot, fileName)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), b...), true
}

// DeleteReport removes the cached report for a repository/file pair.
func (r *Run) DeleteReport(repoRoot, fileName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.reports, FileKey(repoRoot, fileName))
}

// ─── Suggestions ─────────────────────────────────────────────────────────────

// SaveSuggestions stores the suggestions for a repository/file pair. The list
// is copied and sorted by collision key so enumeration is deterministic.
func (r *Run) SaveSuggestions(repoRoot, fileName string, items []Suggestion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sorted := append([]Suggestion(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CollisionKey < sorted[j].CollisionKey
	})
	r.suggestions[FileKey(repoRoot, fileName)] = sorted
}

// SaveSuggestion upserts a single suggestion, keyed by repository + file +
// collision identity, keeping the stored list deterministically ordered. It
// replaces any prior entry for the same collision so repeated generation
// never duplicates records.
func (r *Run) SaveSuggestion(repoRoot string, item Suggestion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fileKey := FileKey(repoRoot, item.File)
	items := r.suggestions[fileKey]

	replaced := false
	for i := range items {
		if items[i].CollisionKey == item.CollisionKey {
			items[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CollisionKey < items[j].CollisionKey
	})
	r.suggestions[fileKey] = items
}

// GetSuggestions returns the suggestions for a repository/file pair.
func (r *Run) GetSuggestions(repoRoot, fileName string) ([]Suggestion, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items, ok := r.suggestions[FileKey(repoRoot, fileName)]
	if !ok {
		return nil, false
	}
	return append([]Suggestion(nil), items...), true
}

// FindSuggestion locates one stored suggestion by repository + file +
// collision identity. It is the dedupe lookup for the suggestion service.
func (r *Run) FindSuggestion(repoRoot, fileName, collisionKey string) (Suggestion, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items, ok := r.suggestions[FileKey(repoRoot, fileName)]
	if !ok {
		return Suggestion{}, false
	}
	for _, item := range items {
		if item.CollisionKey == collisionKey {
			return item, true
		}
	}
	return Suggestion{}, false
}

// TryBeginSuggestion atomically registers an in-flight generation for the
// given run-unique suggestion key. It reports false when a generation for
// the same run/repository/file/collision identity is already in progress,
// which prevents duplicate network requests. Always pair with EndSuggestion.
func (r *Run) TryBeginSuggestion(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.inFlight[key]; busy {
		return false
	}
	r.inFlight[key] = struct{}{}
	return true
}

// EndSuggestion releases an in-flight generation registration.
func (r *Run) EndSuggestion(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inFlight, key)
}
