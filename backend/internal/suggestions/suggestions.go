// Package suggestions generates and stores run-scoped AI resolution
// suggestions.
//
// Phase 2 guarantees implemented here:
//
//   - provider/model come exclusively from the shared ai.Config (never
//     hard-coded); the selected provider and model are visible in logs and
//     in every returned suggestion's metadata,
//   - every AI call is request-scoped: ctx cancellation/timeout is honored
//     and never replaced with context.Background(),
//   - AI concurrency is bounded and never holds locks during network calls,
//   - collision ordering is deterministic regardless of worker completion,
//   - successful suggestions are preserved when other collisions fail
//     (structured partial-failure reporting),
//   - duplicate requests for the same run/repository/file/collision identity
//     are prevented,
//   - suggestions live only in runstate.Run, keyed per collision.
package suggestions

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	ai "CommitIssues/internal/ai"
	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/runstate"
	semantic "CommitIssues/internal/semantic"
)

// DefaultMaxConcurrency bounds concurrent AI requests when the caller does
// not configure a limit.
const DefaultMaxConcurrency = 4

// Meta summarizes one generation pass for API response metadata.
type Meta struct {
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	RunID          string    `json:"runId"`
	Generated      int       `json:"generated"`
	Reused         int       `json:"reused"`
	Deduped        int       `json:"deduped"`
	Failed         int       `json:"failed"`
	BelowThreshold int       `json:"belowThreshold"`
	Retryable      bool      `json:"retryable"`
	Failures       []Failure `json:"failures,omitempty"`
	Config         ai.Config `json:"-"`
	Err            *ai.Error `json:"-"`
}

// Failure is one structured per-collision failure.
type Failure struct {
	File         string `json:"file"`
	CollisionKey string `json:"collisionKey"`
	Code         string `json:"code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
}

// Result is the outcome of a generation pass.
type Result struct {
	Suggestions []runstate.Suggestion
	Meta        Meta
}

// Generator produces run-scoped suggestions for a run's analyses. It is
// stateless: all mutable state lives in the supplied runstate.Run.
type Generator struct {
	// Cfg is the shared AI configuration (flags > env > provider defaults).
	Cfg ai.Config
	// MaxConcurrency bounds concurrent AI requests; <=0 uses the default.
	MaxConcurrency int
	// History optionally receives suggestion counters per run+repository.
	History *runstate.History
}

func (g *Generator) limit() int {
	if g.MaxConcurrency > 0 {
		return g.MaxConcurrency
	}
	return DefaultMaxConcurrency
}

// GenerateForFile generates (or reuses) suggestions for every collision in
// one analyzed file. The file is matched deterministically across
// repositories, exactly like the public API lookup.
func (g *Generator) GenerateForFile(ctx context.Context, run *runstate.Run, repoRoot, file string) (Result, error) {
	if run == nil {
		return Result{}, errors.New("no analysis available to generate suggestions")
	}
	// With an explicit repository, look the analysis up exactly so identical
	// relative paths in different repositories stay isolated. Without one,
	// fall back to the deterministic first match (the documented file-only
	// API lookup behaviour).
	var (
		analysis promptcontext.FileAnalysis
		ok       bool
	)
	if repoRoot != "" {
		analysis, ok = run.GetAnalysis(repoRoot, file)
	}
	if !ok {
		analysis, ok = run.FindAnalysis(file)
	}
	if !ok {
		return Result{}, fmt.Errorf("no analysis found for %s", file)
	}
	if repoRoot == "" {
		repoRoot = analysis.Repository
	}
	return g.generate(ctx, run, []promptcontext.FileAnalysis{analysis}, repoRoot, file)
}

// GenerateForRun generates (or reuses) suggestions for every collision in
// every analysis of the run, preserving deterministic (repository, file,
// collision) ordering.
func (g *Generator) GenerateForRun(ctx context.Context, run *runstate.Run) (Result, error) {
	if run == nil {
		return Result{}, errors.New("no analysis available to generate suggestions")
	}
	analyses := run.AllAnalyses()
	if len(analyses) == 0 {
		return Result{}, errors.New("no analysis available to generate suggestions")
	}
	return g.generate(ctx, run, analyses, "", "")
}

// generate is the shared bounded-concurrency engine.
func (g *Generator) generate(ctx context.Context, run *runstate.Run, analyses []promptcontext.FileAnalysis, singleRepo, singleFile string) (Result, error) {
	meta := Meta{Provider: g.Cfg.Provider, Model: g.Cfg.Model, RunID: run.ID, Config: g.Cfg.Sanitized()}

	// 1. Health check before any generation: typed, actionable, no secrets.
	resolver, err := ai.GetResolver(g.Cfg)
	if err != nil {
		meta.Err = ai.AsError(err)
		return Result{Meta: meta}, err
	}
	if err := ai.CheckProvider(ctx, g.Cfg, nil); err != nil {
		meta.Err = ai.AsError(err)
		return Result{Meta: meta}, err
	}

	// 2. Build the deterministic work plan and reuse stored successes.
	type task struct {
		analysis  promptcontext.FileAnalysis
		collision semantic.DiffItem
		key       string
	}
	var (
		tasks    []task
		reusable []runstate.Suggestion
	)
	for _, analysis := range analyses {
		for _, collision := range analysis.SmartDiff.Collisions {
			key := collisionKey(collision)
			if stored, ok := run.FindSuggestion(analysis.Repository, analysis.File, key); ok {
				switch stored.Status {
				case runstate.StatusComplete, runstate.StatusBelowThreshold:
					reusable = append(reusable, stored)
					meta.Reused++
					continue
				}
			}
			tasks = append(tasks, task{analysis: analysis, collision: collision, key: key})
		}
	}

	// 3. Generate concurrently (bounded), preserving deterministic order.
	results := make([]runstate.Suggestion, len(tasks))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, g.limit())
		mu  sync.Mutex // guards meta counters only; never held during I/O
	)
	for i := range tasks {
		wg.Add(1)
		go func(idx int, t task) {
			defer wg.Done()

			// Bounded concurrency: acquire the slot or abandon on cancel.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			// Duplicate-request prevention per run/repo/file/collision.
			sugKey := runstate.SuggestionKey(t.analysis.Repository, t.analysis.File, t.key)
			if !run.TryBeginSuggestion(sugKey) {
				mu.Lock()
				meta.Deduped++
				mu.Unlock()
				return
			}
			defer run.EndSuggestion(sugKey)

			// Another request may have completed this collision meanwhile.
			if stored, ok := run.FindSuggestion(t.analysis.Repository, t.analysis.File, t.key); ok {
				switch stored.Status {
				case runstate.StatusComplete, runstate.StatusBelowThreshold:
					results[idx] = stored
					mu.Lock()
					meta.Reused++
					mu.Unlock()
					return
				}
			}

			item := g.resolveOne(ctx, resolver, t.analysis, t.collision, t.key)
			run.SaveSuggestion(t.analysis.Repository, item)

			mu.Lock()
			switch item.Status {
			case runstate.StatusComplete:
				meta.Generated++
			case runstate.StatusBelowThreshold:
				meta.Generated++
				meta.BelowThreshold++
			case runstate.StatusFailed:
				meta.Failed++
				meta.Retryable = meta.Retryable || item.Retryable
				meta.Failures = append(meta.Failures, Failure{
					File:         item.File,
					CollisionKey: item.CollisionKey,
					Code:         item.ErrorCode,
					Message:      item.ErrorMessage,
					Retryable:    item.Retryable,
				})
			}
			mu.Unlock()
			results[idx] = item
		}(i, tasks[i])
	}
	wg.Wait()

	// 4. Deterministic assembly: reused + generated, sorted by collision key.
	out := append([]runstate.Suggestion(nil), reusable...)
	for _, item := range results {
		if item.ID != "" || item.Status == runstate.StatusFailed || item.Resolution.Explanation != "" {
			out = append(out, item)
		}
	}
	if singleFile != "" {
		// File-scoped responses only contain that file's suggestions.
		filtered := out[:0:0]
		for _, item := range out {
			if item.File == singleFile && (singleRepo == "" || item.Repository == singleRepo) {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := compareString(out[i].Repository, out[j].Repository); c != 0 {
			return c < 0
		}
		if c := compareString(out[i].File, out[j].File); c != 0 {
			return c < 0
		}
		return out[i].CollisionKey < out[j].CollisionKey
	})

	meta.Failures = sortedFailures(meta.Failures)

	// 5. Record suggestion counters in run history (when wired).
	if g.History != nil {
		g.recordHistory(ctx, run, analyses, meta)
	}

	return Result{Suggestions: out, Meta: meta}, nil
}

// resolveOne performs one validated, retried AI resolution and wraps the
// outcome in a metadata-complete Suggestion. It never panics, never blocks
// on locks, and always records a status.
func (g *Generator) resolveOne(ctx context.Context, resolver ai.Resolver, analysis promptcontext.FileAnalysis, collision semantic.DiffItem, key string) runstate.Suggestion {
	started := time.Now().UTC()
	item := runstate.Suggestion{
		ID:           fmt.Sprintf("%s|%s|%s", analysis.Repository, analysis.File, key),
		File:         analysis.File,
		Repository:   analysis.Repository,
		Collision:    collision,
		CollisionKey: key,
		Provider:     g.Cfg.Provider,
		Model:        g.Cfg.Model,
		Status:       runstate.StatusFailed,
		StartedAt:    started,
	}

	res, err := ai.ResolveCollisionWithRetry(ctx, g.Cfg, resolver, collision, analysis.PromptContext)
	item.CompletedAt = time.Now().UTC()
	if err != nil {
		typed := ai.AsError(err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			typed = ai.AsError(ctxErr)
		}
		item.ErrorCode = string(typed.Code)
		item.Retryable = typed.Retryable
		item.ErrorMessage = typed.Error()
		item.Status = runstate.StatusFailed
		return item
	}

	item.Resolution = *res
	item.Status = runstate.StatusComplete
	if res.Confidence < g.Cfg.ConfidenceThreshold {
		item.Status = runstate.StatusBelowThreshold
	}
	return item
}

// recordHistory merges suggestion counters into the run history entries of
// every repository touched by this generation pass. Timeout/cancellation of
// the parent request is recorded per repository.
func (g *Generator) recordHistory(ctx context.Context, run *runstate.Run, analyses []promptcontext.FileAnalysis, meta Meta) {
	var timedOut, cancelled bool
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		timedOut = true
	case errors.Is(ctx.Err(), context.Canceled):
		cancelled = true
	}

	seen := make(map[string]struct{}, len(analyses))
	for _, analysis := range analyses {
		repo := analysis.Repository
		if _, done := seen[repo]; done {
			continue
		}
		seen[repo] = struct{}{}
		g.History.Merge(run.ID, repo, func(entry *runstate.RunHistory) {
			entry.Provider = meta.Provider
			entry.Model = meta.Model
			entry.CollisionCount = countCollisions(run, repo)
			entry.TimedOut = entry.TimedOut || timedOut
			entry.Cancelled = entry.Cancelled || cancelled

			succeeded, failed, below := 0, 0, 0
			summaries := map[string]struct{}{}
			for _, item := range repoSuggestions(run, repo) {
				switch item.Status {
				case runstate.StatusComplete:
					succeeded++
				case runstate.StatusBelowThreshold:
					below++
				case runstate.StatusFailed:
					failed++
					if item.ErrorMessage != "" {
						summaries[item.ErrorCode+": "+item.ErrorMessage] = struct{}{}
					}
				}
			}
			entry.Succeeded = succeeded
			entry.Failed = failed
			entry.BelowThreshold = below
			merged := make([]string, 0, len(summaries))
			for s := range summaries {
				merged = append(merged, s)
			}
			sort.Strings(merged)
			entry.ErrorSummaries = merged
		})
	}
}

// countCollisions counts collisions across every analyzed file of a repo.
func countCollisions(run *runstate.Run, repoRoot string) int {
	total := 0
	for _, analysis := range run.AllAnalyses() {
		if analysis.Repository == repoRoot {
			total += len(analysis.SmartDiff.Collisions)
		}
	}
	return total
}

// repoSuggestions aggregates stored suggestions for every file of a repo.
func repoSuggestions(run *runstate.Run, repoRoot string) []runstate.Suggestion {
	var out []runstate.Suggestion
	for _, analysis := range run.AllAnalyses() {
		if analysis.Repository != repoRoot {
			continue
		}
		if items, ok := run.GetSuggestions(repoRoot, analysis.File); ok {
			out = append(out, items...)
		}
	}
	return out
}

// collisionKey is the stable per-collision identity used for storage and
// dedupe: the precise symbol identity when available, falling back to the
// legacy kind:name key (mirrors semantic.DiffKey).
func collisionKey(collision semantic.DiffItem) string {
	return semantic.DiffKey(collision)
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func sortedFailures(failures []Failure) []Failure {
	if len(failures) == 0 {
		return nil
	}
	sort.SliceStable(failures, func(i, j int) bool {
		if failures[i].File != failures[j].File {
			return failures[i].File < failures[j].File
		}
		return failures[i].CollisionKey < failures[j].CollisionKey
	})
	return failures
}
