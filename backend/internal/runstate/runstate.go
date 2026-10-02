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
	promptcontext "CommitIssues/internal/context"
	graph "CommitIssues/internal/graph"
	semantic "CommitIssues/internal/semantic"
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

// Suggestion is a run-scoped AI suggestion for a single collision. It is stored
// on the Run rather than in a package-level cache so suggestions can never leak
// across runs or repositories.
type Suggestion struct {
	File       string                  `json:"file"`
	Collision  semantic.DiffItem       `json:"collision"`
	Resolution ai.AIResolutionResponse `json:"resolution"`
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

	mu           sync.RWMutex
	analyses     map[string]promptcontext.FileAnalysis
	prompts      map[string]promptcontext.PromptContextIR
	graphs       map[string]graph.CyGraph
	reports      map[string][]byte
	suggestions  map[string][]Suggestion
	repositoryMD RepositoryMetadata
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
		ID:          id,
		StartedAt:   time.Now().UTC(),
		analyses:    make(map[string]promptcontext.FileAnalysis),
		prompts:     make(map[string]promptcontext.PromptContextIR),
		graphs:      make(map[string]graph.CyGraph),
		reports:     make(map[string][]byte),
		suggestions: make(map[string][]Suggestion),
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

// SaveSuggestions stores the suggestions for a repository/file pair.
func (r *Run) SaveSuggestions(repoRoot, fileName string, items []Suggestion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suggestions[FileKey(repoRoot, fileName)] = append([]Suggestion(nil), items...)
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
