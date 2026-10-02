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
	promptcontext "CommitIssues/internal/context"
	git "CommitIssues/internal/git"
	graph "CommitIssues/internal/graph"
	parser "CommitIssues/internal/parser"
	prompt "CommitIssues/internal/prompt"
	report "CommitIssues/internal/report"
	"CommitIssues/internal/runstate"
	semantic "CommitIssues/internal/semantic"

	sitter "github.com/smacker/go-tree-sitter"
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

	jsParser := sitter.NewParser()
	jsParser.SetLanguage(parser.GetLanguageForFile(targetFile))

	parseSide := func(label string, raw string) (*sitter.Tree, []byte, error) {
		source := parser.NormalizeUTF8([]byte(raw))
		tree, parseErr := jsParser.ParseCtx(ctx, nil, source)
		if parseErr != nil {
			return nil, source, parseErr
		}
		if tree == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, source, ctxErr
			}
			return nil, source, fmt.Errorf("failed to parse %s version into AST", label)
		}
		return tree, source, nil
	}

	ourTree, ourSourceCode, err := parseSide("OUR", conflictData.OurVersion)
	if err != nil {
		return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: CodeFileParse, Err: err}}
	}
	theirTree, theirSourceCode, err := parseSide("THEIR", conflictData.TheirVersion)
	if err != nil {
		return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: CodeFileParse, Err: err}}
	}
	baseTree, baseSourceCode, err := parseSide("BASE", conflictData.BaseVersion)
	if err != nil {
		// A missing/failed base version is not fatal: base may legitimately be
		// empty for add/add style conflicts.
		debugPrintf("[analyze] file %s: base parse skipped: %v\n", targetFile, err)
		baseTree = nil
	}
	debugPrintf("[analyze] file %s: parsed ASTs in %s\n", targetFile, formatDuration(time.Since(start)))

	baseASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	if baseTree != nil {
		parser.ExtractDataForFile(baseTree.RootNode(), baseSourceCode, targetFile, &baseASTData)
	}

	ourASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	parser.ExtractDataForFile(ourTree.RootNode(), ourSourceCode, targetFile, &ourASTData)
	report.PrintASTContext(&buf, "OUR", ourASTData)

	theirASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	parser.ExtractDataForFile(theirTree.RootNode(), theirSourceCode, targetFile, &theirASTData)
	report.PrintASTContext(&buf, "THEIR", theirASTData)

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
		[]string{conflictData.FileName},
		conflictScope,
		ourASTData,
		theirASTData,
	)

	payload := prompt.AIRequestPayload{
		FileName:     conflictData.FileName,
		BaseCode:     string(baseSourceCode),
		OurCode:      string(ourSourceCode),
		TheirCode:    string(theirSourceCode),
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
		run.RegisterAnalysis(repoRoot, promptcontext.FileAnalysis{
			Repository:        repoRoot,
			File:              conflictData.FileName,
			RepositorySummary: fmt.Sprintf("Repository root: %s", repoRoot),
			PromptContext:     promptCtx,
			BaseAST:           baseASTData,
			OurAST:            ourASTData,
			TheirAST:          theirASTData,
			SmartDiff:         smartDiff,
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
		if err := runAIResolution(ctx, &buf, targetFile, smartDiff, promptCtx, cfg); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return FileOutcome{File: targetFile, Output: buf.String(), Err: ctxErr}
			}
			return FileOutcome{File: targetFile, Output: buf.String(), Err: &FileError{File: targetFile, ErrCode: CodeInternal, Err: err}}
		}

		// After AI resolution, release the cached report to free memory.
		if run != nil && reportPath != "" {
			run.DeleteReport(repoRoot, conflictData.FileName)
			reportPath = ""
			debugPrintf("[analyze] file %s: deleted cached report for %s\n", targetFile, conflictData.FileName)
		}
	}

	debugPrintf("[analyze] file %s: finished in %s\n", targetFile, formatDuration(time.Since(start)))
	return FileOutcome{File: targetFile, Output: buf.String(), ReportPath: reportPath}
}

// runAIResolution resolves each collision in order, honoring cancellation.
func runAIResolution(ctx context.Context, buf *bytes.Buffer, targetFile string, smartDiff semantic.SmartDiffResult, promptCtx promptcontext.PromptContextIR, cfg Config) error {
	debugPrintf("[analyze] file %s: starting AI resolution for %d collision(s) via %s\n", targetFile, len(smartDiff.Collisions), cfg.Provider)
	fmt.Fprintf(buf, "\nInitializing AI Provider: %s...\n", cfg.Provider)

	aiConfig := ai.AIConfig{
		Provider: cfg.Provider,
		Model:    cfg.Model,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
	}

	resolver, err := ai.GetResolver(aiConfig)
	if err != nil {
		debugPrintf("[analyze] file %s: AI provider initialization failed: %v\n", targetFile, err)
		fmt.Fprintf(buf, "Failed to initialize AI provider: %v\n", err)
		return nil
	}

	for i, collision := range smartDiff.Collisions {
		if err := ctx.Err(); err != nil {
			return err
		}
		collisionStart := time.Now()
		fmt.Fprintf(buf, "  -> Requesting AI resolution for [%s] %s (Line %d)...\n", collision.Kind, collision.Name, collision.Line)

		res, aiErr := resolver.ResolveCollision(ctx, collision, promptCtx)
		if aiErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			debugPrintf("[analyze] file %s: collision %d/%d failed after %s: %v\n", targetFile, i+1, len(smartDiff.Collisions), formatDuration(time.Since(collisionStart)), aiErr)
			fmt.Fprintf(buf, "     ⚠ AI Error: %v\n", aiErr)
			continue
		}

		debugPrintf("[analyze] file %s: collision %d/%d resolved in %s (confidence=%d%%)\n", targetFile, i+1, len(smartDiff.Collisions), formatDuration(time.Since(collisionStart)), res.Confidence)
		fmt.Fprintf(buf, "     ✓ Resolved (Confidence: %d%%)\n", res.Confidence)
		fmt.Fprintf(buf, "       Explanation: %s\n", res.Explanation)
		fmt.Fprintf(buf, "       Suggested Code:\n")
		for _, line := range strings.Split(res.SuggestedCode, "\n") {
			fmt.Fprintf(buf, "         %s\n", line)
		}
	}
	return nil
}

func runID(run *runstate.Run) string {
	if run == nil {
		return ""
	}
	return run.ID
}
