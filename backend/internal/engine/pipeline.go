package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/cache"
	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
	graph "CommitIssues/internal/graph"
	parser "CommitIssues/internal/parser"
	prompt "CommitIssues/internal/prompt"
	report "CommitIssues/internal/report"
	"CommitIssues/internal/resolutions"
	"CommitIssues/internal/runstate"
	semantic "CommitIssues/internal/semantic"
)

// FileOutcome is the result of processing a single conflicted file. Errors are
// always structured (see *FileError) so a single bad file cannot terminate the
// whole run.
type FileOutcome struct {
	File       string
	Output     string
	ReportPath string
	Err        error
}

// RunResult summarizes one repository processing pass.
type RunResult struct {
	RunID      string
	Repository string
	Duration   time.Duration
	// Outcomes preserves deterministic file order.
	Outcomes  []FileOutcome
	Succeeded []FileOutcome
	Failed    []FileOutcome
}

// Summary renders a deterministic, human-readable run summary.
func (r *RunResult) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "run %s: %d file(s) processed (%d succeeded, %d failed) in %s\n",
		r.RunID, len(r.Outcomes), len(r.Succeeded), len(r.Failed), formatDuration(r.Duration))
	for _, failed := range r.Failed {
		fmt.Fprintf(&b, "  - failed: %v\n", failed.Err)
	}
	return b.String()
}

var debugLoggingEnabled bool

// EnableDebugLogging toggles verbose pipeline logging.
func EnableDebugLogging(enabled bool) {
	debugLoggingEnabled = enabled
}

func debugPrintf(format string, args ...interface{}) {
	if !debugLoggingEnabled {
		return
	}
	fmt.Printf(format, args...)
}

// ResolveScanRoot resolves a user-supplied path to a scan root directory.
func ResolveScanRoot(argPath string) string {
	if argPath != "" {
		if filepath.IsAbs(argPath) {
			return argPath
		}
		cwd, err := os.Getwd()
		if err == nil {
			if _, err := os.Stat(filepath.Join(cwd, argPath)); err == nil {
				return filepath.Join(cwd, argPath)
			}
			if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
				parent := filepath.Dir(cwd)
				if _, err := os.Stat(filepath.Join(parent, argPath)); err == nil {
					return filepath.Join(parent, argPath)
				}
			}
		}
		return argPath
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
		return filepath.Dir(cwd)
	}
	return cwd
}

// FindConflicts returns a map of repoRoot -> conflicted files plus the
// deterministically sorted repository roots.
func FindConflicts(ctx context.Context, scanRoot string) (map[string][]string, []string, error) {
	repoRoots, err := git.FindGitRepositoryRoots(ctx, scanRoot)
	if err != nil {
		return nil, nil, err
	}

	absScanRoot, err := filepath.Abs(scanRoot)
	if err != nil {
		absScanRoot = scanRoot
	}

	conflictsByRepo := make(map[string][]string)
	for _, repoRoot := range repoRoots {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		conflictedFiles, err := git.GetConflictedFiles(ctx, repoRoot)
		if err != nil {
			// A discovery failure (Git error, filesystem error, cancellation) must
			// never be reported as a successful scan.
			return nil, nil, err
		}
		if len(conflictedFiles) == 0 {
			continue
		}

		absRepoRoot, absErr := filepath.Abs(repoRoot)
		if absErr != nil {
			absRepoRoot = repoRoot
		}

		// If scanRoot is a subdirectory of repoRoot, filter files.
		relScan, relErr := filepath.Rel(absRepoRoot, absScanRoot)
		if relErr == nil && relScan != "." && !strings.HasPrefix(relScan, "..") {
			var filtered []string
			prefix := relScan + string(filepath.Separator)
			for _, f := range conflictedFiles {
				cleanF := filepath.Clean(f)
				if cleanF == relScan || strings.HasPrefix(cleanF, prefix) {
					filtered = append(filtered, f)
				}
			}
			conflictedFiles = filtered
		}

		if len(conflictedFiles) > 0 {
			sort.Strings(conflictedFiles)
			conflictsByRepo[repoRoot] = conflictedFiles
		}
	}

	sort.Strings(repoRoots)
	return conflictsByRepo, repoRoots, nil
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

// ProcessRepository analyzes every conflicted file in a repository using a
// bounded, cancellation-aware worker pool.
//
// A single file failure is collected as a structured per-file error and never
// terminates the run. Terminal errors are returned only for invalid
// configuration, fatal initialization failure, cancellation or timeout.
func ProcessRepository(ctx context.Context, run *runstate.Run, repoRoot string, conflictedFiles []string, cfg Config, runAI bool) (*RunResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// One AST cache per run: incremental reuse within the run, no shared
	// state across runs. Config is passed by value, so this stays local.
	if cfg.ASTCache == nil {
		cfg.ASTCache = cache.NewDefault()
	}

	// Load the README once per repository before the worker pool so every
	// file in this repo sees the same excerpt without redundant I/O.
	// Missing README and disabled config both produce an empty string — no
	// README section is rendered and the prompt budget is unaffected.
	if run != nil && cfg.ReadmeContext {
		excerpt, _, readmeErr := promptcontext.LoadReadmeContext(repoRoot, cfg.ReadmeMaxBytes)
		if readmeErr != nil {
			debugPrintf("[analyze] repo %s: README load error (non-fatal): %v\n", repoRoot, readmeErr)
		}
		run.RegisterReadme(repoRoot, excerpt)
	} else if run != nil {
		// Feature disabled: register an empty excerpt so workers can read
		// consistently via run.Readme() without a missing-key branch.
		run.RegisterReadme(repoRoot, "")
	}

	files := append([]string(nil), conflictedFiles...)
	sort.Strings(files)

	result := &RunResult{RunID: runID(run), Repository: repoRoot}
	outcomes := make([]FileOutcome, len(files))
	for i, file := range files {
		outcomes[i] = FileOutcome{File: file}
	}

	start := time.Now()
	debugPrintf("[analyze] repo %s: starting %d file(s) in parallel (max concurrency=%d)\n", repoRoot, len(files), cfg.MaxConcurrency)

	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.MaxConcurrency)

	for i, file := range files {
		wg.Add(1)
		go func(idx int, target string) {
			defer wg.Done()

			// Acquire the semaphore, but abandon immediately on cancellation
			// so no new work is scheduled and no worker blocks forever.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				outcomes[idx] = FileOutcome{File: target, Err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			outcomes[idx] = ProcessConflictFile(ctx, run, repoRoot, target, cfg, runAI)
		}(i, file)
	}
	wg.Wait()
	result.Duration = time.Since(start)

	result.Outcomes = outcomes
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			result.Failed = append(result.Failed, outcome)
		} else {
			result.Succeeded = append(result.Succeeded, outcome)
		}
	}

	debugPrintf("[analyze] repo %s: finished in %s\n", repoRoot, formatDuration(result.Duration))

	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}


// ProcessConflictFile runs AST extraction, semantic diff, graphing and
// optionally AI resolution for a single conflicted file.
func ProcessConflictFile(ctx context.Context, run *runstate.Run, repoRoot, targetFile string, cfg Config, runAI bool) FileOutcome {
	start := time.Now()
	debugPrintf("[analyze] file %s: starting processing\n", targetFile)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n--- Conflict: %s ---\n", targetFile)

	if err := ctx.Err(); err != nil {
		return FileOutcome{File: targetFile, Output: buf.String(), Err: err}
	}

	conflictData, err := git.ExtractConflictVersions(ctx, repoRoot, targetFile)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return FileOutcome{File: targetFile, Output: buf.String(), Err: ctxErr}
		}
		code := CodeFileExtract
		var missingStage *git.MissingStageError
		if errors.As(err, &missingStage) {
			code = CodeGitStage
		}
		return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: code, Err: err}}
	}
	debugPrintf("[analyze] file %s: extracted versions in %s (source=%s, regions=%d)\n",
		targetFile, formatDuration(time.Since(start)), conflictData.Source, len(conflictData.Regions))
	fmt.Fprintln(&buf, "Successfully extracted Base, Ours, and Theirs code.")

	lang := parser.GetLanguageForFile(targetFile)
	baseASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	ourASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	theirASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}

	// Defensive fallback for direct callers bypassing ProcessRepository:
	// a per-call cache, never a package-global one.
	astCache := cfg.ASTCache
	if astCache == nil {
		astCache = cache.NewDefault()
	}

	if lang != nil {
		var err error
		ourASTData, _, err = astCache.GetOrParse(ctx, repoRoot, targetFile, []byte(conflictData.OurVersion))
		if err != nil {
			return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: CodeFileParse, Err: err}}
		}
		theirASTData, _, err = astCache.GetOrParse(ctx, repoRoot, targetFile, []byte(conflictData.TheirVersion))
		if err != nil {
			return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: CodeFileParse, Err: err}}
		}
		if conflictData.BaseVersion != "" {
			baseASTData, _, err = astCache.GetOrParse(ctx, repoRoot, targetFile, []byte(conflictData.BaseVersion))
			if err != nil {
				// A missing/failed base version is not fatal: base may legitimately be
				// empty for add/add style conflicts.
				debugPrintf("[analyze] file %s: base parse skipped: %v\n", targetFile, err)
			}
		}
		debugPrintf("[analyze] file %s: parsed ASTs in %s\n", targetFile, formatDuration(time.Since(start)))

		report.PrintASTContext(&buf, "OUR", ourASTData)
		report.PrintASTContext(&buf, "THEIR", theirASTData)
	} else {
		debugPrintf("[analyze] file %s: unsupported language, using text fallback\n", targetFile)
		fmt.Fprintf(&buf, "File %s has unsupported language grammar; using text fallback.\n", targetFile)
	}

	smartDiff, err := semantic.GenerateSmartDiffContext(ctx, baseASTData, ourASTData, theirASTData)
	if err != nil {
		return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: CodeInternal, Err: err}}
	}
	debugPrintf("[analyze] file %s: generated smart diff in %s (collisions=%d, ours=%d, theirs=%d)\n",
		targetFile, formatDuration(time.Since(start)), len(smartDiff.Collisions), len(smartDiff.OurChanges), len(smartDiff.TheirChanges))

	ourSemanticGraph := semantic.BuildSemanticGraph(ourASTData)
	theirSemanticGraph := semantic.BuildSemanticGraph(theirASTData)
	mergedSemanticGraph := semantic.MergeSemanticGraphs(ourSemanticGraph, theirSemanticGraph)
	conflictScope := semantic.ComputeConflictScope(mergedSemanticGraph, smartDiff.Collisions)

	promptCtx := promptcontext.BuildPromptContext(
		fmt.Sprintf("Repository root: %s", repoRoot),
		readmeExcerpt(run, repoRoot),
		[]string{conflictData.FileName},
		conflictScope,
		ourASTData,
		theirASTData,
	)
	// Soft prompt budget: trim auxiliary context (README → functions →
	// variables) without ever touching the collision payload.
	promptCtx = promptcontext.ApplyTargetBudget(promptCtx, cfg.AITargetPromptTokens)

	payload := prompt.AIRequestPayload{
		FileName:     conflictData.FileName,
		BaseCode:     conflictData.BaseVersion,
		OurCode:      conflictData.OurVersion,
		TheirCode:    conflictData.TheirVersion,
		OurASTData:   ourASTData,
		TheirASTData: theirASTData,
		SmartDiff:    smartDiff,
	}
	jsonBytes, err := prompt.MarshalAIRequestPayload(payload)
	if err != nil {
		fmt.Fprintf(&buf, "Failed to marshal AI request payload for %s: %v\n", payload.FileName, err)
	} else {
		report.PrintPayloadJSON(&buf, payload, jsonBytes)
	}

	// Register run-scoped state only once every deterministic input is computed.
	// All keys are repository-safe so identical relative paths in different
	// repositories never overwrite one another.
	if run != nil {
		run.RegisterPromptContext(repoRoot, conflictData.FileName, promptCtx)
		run.RegisterGraph(repoRoot, conflictData.FileName, graph.BuildCyGraph(repoRoot, conflictData.FileName, smartDiff, conflictScope))

		// ── Phase 2: compute working-tree hashes and capture conflict regions ──
		var (
			contentHash         string
			regionContextHashes map[string]string
			wtRegions           []git.ConflictRegion
		)
		absFilePath := filepath.Join(repoRoot, targetFile)
		if ch, rh, hashErr := fileutil.RegionContextHashesForFile(absFilePath); hashErr == nil {
			contentHash = ch
			regionContextHashes = rh
		} else {
			debugPrintf("[analyze] file %s: Phase 2 hash skipped: %v\n", targetFile, hashErr)
		}
		// Use the regions already parsed from the on-disk file when source is
		// inline. For staged files, re-read the working-tree copy (which still
		// has markers at apply time) for accurate region capture.
		if conflictData.Source == git.SourceInline {
			wtRegions = conflictData.Regions
		} else if data, readErr := os.ReadFile(absFilePath); readErr == nil {
			wtRegions = git.ParseConflictRegions(string(data)).Regions
		}

		run.RegisterAnalysis(repoRoot, promptcontext.FileAnalysis{
			Repository:          repoRoot,
			File:                conflictData.FileName,
			RepositorySummary:   fmt.Sprintf("Repository root: %s", repoRoot),
			PromptContext:       promptCtx,
			BaseAST:             baseASTData,
			OurAST:              ourASTData,
			TheirAST:            theirASTData,
			SmartDiff:           smartDiff,
			ContentHash:         contentHash,
			ConflictRegions:     wtRegions,
			RegionContextHashes: regionContextHashes,
			ParserVersion:       fileutil.ParserVersion,
			AnalysisTimestamp:   time.Now().UTC(),
		})
	}

	var reportPath string
	if jsonBytes != nil && run != nil {
		reportPath = run.SaveReport(repoRoot, conflictData.FileName, jsonBytes)
		if reportPath == "" {
			fmt.Fprintf(&buf, "Warning: failed to cache report for %s\n", conflictData.FileName)
		}
	}

	report.PrintDiffReport(&buf, smartDiff)

	if runAI {
		if err := runAIResolution(ctx, run, repoRoot, &buf, targetFile, smartDiff, promptCtx, cfg); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return FileOutcome{File: targetFile, Output: buf.String(), Err: ctxErr}
			}
			return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: CodeInternal, Err: err}}
		}

		// The cached report is intentionally retained after AI resolution: it
		// is part of the run's audit trail (suggestions and resolutions
		// reference the analysis snapshot). Reports are run-scoped and bounded
		// by the number of conflicted files in the run.
	}

	debugPrintf("[analyze] file %s: finished in %s\n", targetFile, formatDuration(time.Since(start)))
	return FileOutcome{File: targetFile, Output: buf.String(), ReportPath: reportPath}
}

// runAIResolution resolves each collision in order, honoring cancellation,
// applying the configured health check, per-request timeout and retry policy,
// and recording every outcome as a run-scoped suggestion.
func runAIResolution(ctx context.Context, run *runstate.Run, repoRoot string, buf *bytes.Buffer, targetFile string, smartDiff semantic.SmartDiffResult, promptCtx promptcontext.PromptContextIR, cfg Config) error {
	debugPrintf("[analyze] file %s: starting AI resolution for %d collision(s) via %s\n", targetFile, len(smartDiff.Collisions), cfg.AI.Provider)
	fmt.Fprintf(buf, "\nInitializing AI Provider: %s (model: %s)...\n", cfg.AI.Provider, cfg.AI.Model)

	// The engine-level confidence threshold governs the pipeline; keep the
	// shared AI config in sync so suggestion statuses use one source.
	aiConfig := cfg.AI
	aiConfig.ConfidenceThreshold = cfg.ConfidenceThreshold

	resolver, err := ai.GetResolver(aiConfig)
	if err != nil {
		debugPrintf("[analyze] file %s: AI provider initialization failed: %v\n", targetFile, err)
		fmt.Fprintf(buf, "Failed to initialize AI provider: %v\n", err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		recordAISetupFailures(run, repoRoot, targetFile, smartDiff, aiConfig, err)
		return nil
	}

	// Provider health check before generation: typed and actionable.
	if err := ai.CheckProvider(ctx, aiConfig, nil); err != nil {
		debugPrintf("[analyze] file %s: AI provider health check failed: %v\n", targetFile, err)
		fmt.Fprintf(buf, "AI provider unavailable: %v\n", err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		recordAISetupFailures(run, repoRoot, targetFile, smartDiff, aiConfig, err)
		return nil
	}

	for i, collision := range smartDiff.Collisions {
		if err := ctx.Err(); err != nil {
			return err
		}
		collisionStart := time.Now()
		fmt.Fprintf(buf, "  -> Requesting AI resolution for [%s] %s (Line %d)...\n", collision.Kind, collision.Name, collision.Line)

		started := time.Now().UTC()
		res, aiErr := ai.ResolveCollisionWithRetry(ctx, aiConfig, resolver, collision, promptCtx)
		duration := formatDuration(time.Since(collisionStart))

		if run != nil {
			analysis, _ := run.GetAnalysis(repoRoot, targetFile)
			region, hasRegion := git.MatchConflictRegion(analysis.ConflictRegions, collision.Line, collision.OurContent, collision.TheirContent, collision.BaseContent)
			var regionID string
			if hasRegion {
				regionID = fmt.Sprintf("%d", region.Index)
			}

			item := runstate.Suggestion{
				ID:           runstate.SuggestionKey(repoRoot, targetFile, semantic.DiffKey(collision)),
				File:         targetFile,
				Repository:   repoRoot,
				Collision:    collision,
				CollisionKey: semantic.DiffKey(collision),
				Revision:     1,
				RegionID:     regionID,
				Provider:     aiConfig.Provider,
				Model:        aiConfig.Model,
				StartedAt:    started,
				CompletedAt:  time.Now().UTC(),
			}
			if aiErr != nil {
				typed := ai.AsError(aiErr)
				if ctxErr := ctx.Err(); ctxErr != nil {
					typed = ai.AsError(ctxErr)
				}
				item.Status = runstate.StatusFailed
				item.ErrorCode = string(typed.Code)
				item.Retryable = typed.Retryable
				item.ErrorMessage = typed.Error()
			} else {
				item.Resolution = *res
				item.Status = runstate.StatusComplete
				if res.Confidence < cfg.ConfidenceThreshold {
					item.Status = runstate.StatusBelowThreshold
				}
				contextHash := ""
				if analysis.RegionContextHashes != nil {
					contextHash = analysis.RegionContextHashes[regionID]
				}
				resolutionRecord := resolutions.Resolution{
					ID:               resolutions.NewID(),
					SuggestionID:     item.ID,
					RunID:            run.ID,
					Repository:       repoRoot,
					File:             targetFile,
					CollisionKey:     semantic.DiffKey(collision),
					Revision:         1,
					RegionID:         regionID,
					StartLine:        region.StartLine,
					EndLine:          region.EndLine,
					ContentHash:      analysis.ContentHash,
					ContextHash:      contextHash,
					Base:             region.Base,
					Ours:             region.Ours,
					Theirs:           region.Theirs,
					Replacement:      res.SuggestedCode,
					Status:           resolutions.StatusProposed,
					ApprovalStatus:   resolutions.ApprovalNone,
					ValidationStatus: resolutions.ValidationNotRun,
					CreatedAt:        time.Now().UTC(),
				}
				savedRes := run.SaveResolution(resolutionRecord)
				item.ResolutionID = savedRes.ID
			}
			run.SaveSuggestion(repoRoot, item)
		}

		if aiErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			debugPrintf("[analyze] file %s: collision %d/%d failed after %s: %v\n", targetFile, i+1, len(smartDiff.Collisions), duration, aiErr)
			fmt.Fprintf(buf, "     ⚠ AI Error: %v\n", aiErr)
			continue
		}

		debugPrintf("[analyze] file %s: collision %d/%d resolved in %s (confidence=%d%%)\n", targetFile, i+1, len(smartDiff.Collisions), duration, res.Confidence)
		fmt.Fprintf(buf, "     ✓ Resolved (Confidence: %d%%)\n", res.Confidence)
		fmt.Fprintf(buf, "       Explanation: %s\n", res.Explanation)
		fmt.Fprintf(buf, "       Suggested Code:\n")
		for _, line := range strings.Split(res.SuggestedCode, "\n") {
			fmt.Fprintf(buf, "         %s\n", line)
		}
	}
	return nil
}

func recordAISetupFailures(run *runstate.Run, repoRoot, targetFile string, smartDiff semantic.SmartDiffResult, cfg ai.Config, err error) {
	if run == nil {
		return
	}
	typed := ai.AsError(err)
	now := time.Now().UTC()
	for _, collision := range smartDiff.Collisions {
		key := semantic.DiffKey(collision)
		run.SaveSuggestion(repoRoot, runstate.Suggestion{
			ID:           runstate.SuggestionKey(repoRoot, targetFile, key),
			File:         targetFile,
			Repository:   repoRoot,
			Collision:    collision,
			CollisionKey: key,
			Provider:     cfg.Provider,
			Model:        cfg.Model,
			Status:       runstate.StatusFailed,
			ErrorCode:    string(typed.Code),
			Retryable:    typed.Retryable,
			ErrorMessage: typed.Error(),
			StartedAt:    now,
			CompletedAt:  now,
		})
	}
}

func runID(run *runstate.Run) string {
	if run == nil {
		return ""
	}
	return run.ID
}

// readmeExcerpt retrieves the README excerpt registered for a repository by
// ProcessRepository. Returns an empty string when run is nil (direct test
// callers that bypass ProcessRepository) or when no excerpt was stored.
func readmeExcerpt(run *runstate.Run, repoRoot string) string {
	if run == nil {
		return ""
	}
	excerpt, _ := run.Readme(repoRoot)
	return excerpt
}

