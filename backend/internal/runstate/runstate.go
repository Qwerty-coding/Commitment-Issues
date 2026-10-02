// Package runstate holds all mutable state produced by a single analysis run.
//
// Previously analysis, prompt context, graph and report data lived in global
// package-level variables, which leaked between runs and duplicated graph
// objects on repeated scans. A Run is now created per execution, owns an
// isolated set of maps, and returns every collection in a deterministic,
// sorted order.
package runstate

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	promptcontext "CommitIssues/internal/context"
	graph "CommitIssues/internal/graph"
)

// Run owns the isolated state for one analysis run.
type Run struct {
	ID        string
	StartedAt time.Time

	mu       sync.RWMutex
	analyses map[string]promptcontext.FileAnalysis
	prompts  map[string]promptcontext.PromptContextIR
	graphs   map[string]graph.CyGraph
	reports  map[string][]byte
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
		ID:        id,
		StartedAt: time.Now().UTC(),
		analyses:  make(map[string]promptcontext.FileAnalysis),
		prompts:   make(map[string]promptcontext.PromptContextIR),
		graphs:    make(map[string]graph.CyGraph),
		reports:   make(map[string][]byte),
	}
}

func newRunID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(buf)
}

// ─── Analyses ────────────────────────────────────────────────────────────────

// RegisterAnalysis stores the analysis for a file, replacing any prior entry so
// that repeated scans do not duplicate data.
func (r *Run) RegisterAnalysis(analysis promptcontext.FileAnalysis) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.analyses[analysis.File] = analysis
}

// GetAnalysis returns the analysis for a file.
func (r *Run) GetAnalysis(file string) (promptcontext.FileAnalysis, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	analysis, ok := r.analyses[file]
	return analysis, ok
}

// AllAnalyses returns every analysis sorted by file name.
func (r *Run) AllAnalyses() []promptcontext.FileAnalysis {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]promptcontext.FileAnalysis, 0, len(r.analyses))
	for _, analysis := range r.analyses {
		out = append(out, analysis)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// ─── Prompt contexts ─────────────────────────────────────────────────────────

// RegisterPromptContext stores the prompt context for a file.
func (r *Run) RegisterPromptContext(fileName string, ctx promptcontext.PromptContextIR) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts[fileName] = ctx
}

// GetPromptContext returns the prompt context for a file.
func (r *Run) GetPromptContext(fileName string) (promptcontext.PromptContextIR, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ctx, ok := r.prompts[fileName]
	return ctx, ok
}

// AllPromptContexts returns every prompt context sorted by file name.
func (r *Run) AllPromptContexts() []promptcontext.PromptContextIR {
	r.mu.RLock()
	defer r.mu.RUnlock()
	files := make([]string, 0, len(r.prompts))
	for file := range r.prompts {
		files = append(files, file)
	}
	sort.Strings(files)
	out := make([]promptcontext.PromptContextIR, 0, len(files))
	for _, file := range files {
		out = append(out, r.prompts[file])
	}
	return out
}

// ─── Graphs ──────────────────────────────────────────────────────────────────

// RegisterGraph stores the graph for a file, replacing any prior graph for that
// file so repeated scans never duplicate nodes.
func (r *Run) RegisterGraph(file string, g graph.CyGraph) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.graphs[file] = g
}

// MergedGraph returns the deterministic, deduplicated union of every graph.
func (r *Run) MergedGraph() graph.CyGraph {
	r.mu.RLock()
	files := make([]string, 0, len(r.graphs))
	for file := range r.graphs {
		files = append(files, file)
	}
	sort.Strings(files)
	graphs := make([]graph.CyGraph, 0, len(files))
	for _, file := range files {
		graphs = append(graphs, r.graphs[file])
	}
	r.mu.RUnlock()
	return graph.MergeGraphs(graphs)
}

// MergedGraphDTO returns the merged graph flattened for the API.
func (r *Run) MergedGraphDTO() graph.GraphDTO {
	return r.MergedGraph().ToDTO()
}

// GraphFiles returns the sorted list of files with a registered graph.
func (r *Run) GraphFiles() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	files := make([]string, 0, len(r.graphs))
	for file := range r.graphs {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

// ─── Reports ─────────────────────────────────────────────────────────────────

func reportKey(repoRoot, fileName string) string {
	return repoRoot + "|" + fileName
}

// SaveReport stores report bytes for a repository/file pair.
func (r *Run) SaveReport(repoRoot, fileName string, jsonBytes []byte) string {
	key := reportKey(repoRoot, fileName)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports[key] = append([]byte(nil), jsonBytes...)
	return key
}

// GetReport retrieves report bytes for a repository/file pair.
func (r *Run) GetReport(repoRoot, fileName string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.reports[reportKey(repoRoot, fileName)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), b...), true
}

// DeleteReport removes the cached report for a repository/file pair.
func (r *Run) DeleteReport(repoRoot, fileName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.reports, reportKey(repoRoot, fileName))
}
