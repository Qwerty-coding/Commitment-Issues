package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/cache"
	promptcontext "CommitIssues/internal/context"
	git "CommitIssues/internal/git"
	graph "CommitIssues/internal/graph"
	parser "CommitIssues/internal/parser"
	prompt "CommitIssues/internal/prompt"
	report "CommitIssues/internal/report"
	semantic "CommitIssues/internal/semantic"

	sitter "github.com/smacker/go-tree-sitter"
)

type Config struct {
	MaxConcurrency      int
	ConfidenceThreshold int
	APIKey              string
	Provider            string
	Model               string
	BaseURL             string
}

type FileOutcome struct {
	Output     string
	ReportPath string
	Err        error
}

var debugLoggingEnabled bool

func EnableDebugLogging(enabled bool) {
	debugLoggingEnabled = enabled
}

func debugPrintf(format string, args ...interface{}) {
	if !debugLoggingEnabled {
		return
	}
	fmt.Printf(format, args...)
}

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

// FindConflicts returns map of repoRoot -> conflictedFiles
func FindConflicts(scanRoot string) (map[string][]string, []string, error) {
	repoRoots, err := git.FindGitRepositoryRoots(scanRoot)
	if err != nil {
		return nil, nil, err
	}

	absScanRoot, err := filepath.Abs(scanRoot)
	if err != nil {
		absScanRoot = scanRoot
	}

	conflictsByRepo := make(map[string][]string)
	for _, repoRoot := range repoRoots {
		conflictedFiles, err := git.GetConflictedFiles(repoRoot)
		if err == nil && len(conflictedFiles) > 0 {
			absRepoRoot, err := filepath.Abs(repoRoot)
			if err != nil {
				absRepoRoot = repoRoot
			}

			// If scanRoot is a subdirectory of repoRoot, filter files
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
				conflictsByRepo[repoRoot] = conflictedFiles
			}
		}
	}
	return conflictsByRepo, repoRoots, nil
}

// ProcessRepository conflicts concurrently
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

func ProcessRepository(repoRoot string, conflictedFiles []string, cfg Config, runAI bool) []FileOutcome {
	start := time.Now()
	debugPrintf("[resolve] repo %s: starting %d file(s) in parallel (max concurrency=%d)\n", repoRoot, len(conflictedFiles), cfg.MaxConcurrency)

	outcomes := make([]FileOutcome, len(conflictedFiles))
	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.MaxConcurrency)

	for i, targetFile := range conflictedFiles {
		wg.Add(1)
		go func(idx int, file string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Inform user immediately that processing for this file has started.
			fmt.Printf("Processing %s...\n", file)
			debugPrintf("[resolve] repo %s: starting file %s\n", repoRoot, file)
			outcomes[idx] = ProcessConflictFile(repoRoot, file, cfg, runAI)
			// Stream the file's output as soon as it's ready so users see progress.
			if outcomes[idx].Output != "" {
				fmt.Print(outcomes[idx].Output)
			}
			debugPrintf("[resolve] repo %s: completed file %s\n", repoRoot, file)
		}(i, targetFile)
	}
	wg.Wait()
	debugPrintf("[resolve] repo %s: finished all files in %s\n", repoRoot, formatDuration(time.Since(start)))
	return outcomes
}

// ProcessConflictFile runs AST extraction, Semantic Diff, Graphing, & optionally AI
func ProcessConflictFile(repoRoot, targetFile string, cfg Config, runAI bool) FileOutcome {
	start := time.Now()
	debugPrintf("[resolve] file %s: starting processing\n", targetFile)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n--- Conflict: %s ---\n", targetFile)

	conflictData, err := git.ExtractConflictVersions(repoRoot, targetFile)
	if err != nil {
		debugPrintf("[resolve] file %s: failed to extract versions in %s: %v\n", targetFile, formatDuration(time.Since(start)), err)
		return FileOutcome{Output: buf.String(), Err: fmt.Errorf("failed to extract versions: %w", err)}
	}
	debugPrintf("[resolve] file %s: extracted versions in %s\n", targetFile, formatDuration(time.Since(start)))
	fmt.Fprintln(&buf, "Successfully extracted Base, Ours, and Theirs code from Git index.")

	jsParser := sitter.NewParser()
	jsParser.SetLanguage(parser.GetLanguageForFile(targetFile))
	debugPrintf("[resolve] file %s: using parser for extension %q\n", targetFile, filepath.Ext(targetFile))

	ourSourceCode := parser.NormalizeUTF8([]byte(conflictData.OurVersion))
	ourTree, _ := jsParser.ParseCtx(context.Background(), nil, ourSourceCode)
	if ourTree == nil {
		debugPrintf("[resolve] file %s: failed to parse OUR version in %s\n", targetFile, formatDuration(time.Since(start)))
		return FileOutcome{Output: buf.String(), Err: fmt.Errorf("failed to parse OUR version into AST")}
	}

	theirSourceCode := parser.NormalizeUTF8([]byte(conflictData.TheirVersion))
	theirTree, _ := jsParser.ParseCtx(context.Background(), nil, theirSourceCode)
	if theirTree == nil {
		debugPrintf("[resolve] file %s: failed to parse THEIR version in %s\n", targetFile, formatDuration(time.Since(start)))
		return FileOutcome{Output: buf.String(), Err: fmt.Errorf("failed to parse THEIR version into AST")}
	}

	baseSourceCode := parser.NormalizeUTF8([]byte(conflictData.BaseVersion))
	baseTree, _ := jsParser.ParseCtx(context.Background(), nil, baseSourceCode)
	debugPrintf("[resolve] file %s: parsed ASTs in %s\n", targetFile, formatDuration(time.Since(start)))

	baseASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	if baseTree != nil {
		parser.ExtractData(baseTree.RootNode(), baseSourceCode, &baseASTData)
	}

	ourASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	parser.ExtractData(ourTree.RootNode(), ourSourceCode, &ourASTData)
	report.PrintASTContext(&buf, "OUR", ourASTData)

	theirASTData := parser.ASTContext{Functions: []parser.CodeElement{}, Variables: []parser.CodeElement{}}
	parser.ExtractData(theirTree.RootNode(), theirSourceCode, &theirASTData)
	report.PrintASTContext(&buf, "THEIR", theirASTData)

	smartDiff := semantic.GenerateSmartDiff(baseASTData, ourASTData, theirASTData)
	debugPrintf("[resolve] file %s: generated smart diff in %s (collisions=%d, ours=%d, theirs=%d)\n", targetFile, formatDuration(time.Since(start)), len(smartDiff.Collisions), len(smartDiff.OurChanges), len(smartDiff.TheirChanges))
	ourSemanticGraph := semantic.BuildSemanticGraph(ourASTData)
	theirSemanticGraph := semantic.BuildSemanticGraph(theirASTData)
	mergedSemanticGraph := semantic.MergeSemanticGraphs(ourSemanticGraph, theirSemanticGraph)
	conflictScope := semantic.ComputeConflictScope(mergedSemanticGraph, smartDiff.Collisions)
	promptContext := promptcontext.BuildPromptContext(
		fmt.Sprintf("Repository root: %s", repoRoot),
		[]string{conflictData.FileName},
		conflictScope,
		ourASTData,
		theirASTData,
	)
	promptcontext.RegisterPromptContext(conflictData.FileName, promptContext)
	graph.RegisterGraph(graph.BuildCyGraph(conflictData.FileName, smartDiff, conflictScope))
	report.PrintDiffReport(&buf, smartDiff)

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

	var reportPath string
	// Register analysis (without storing token statistics) so API can read contextual info
	promptcontext.RegisterAnalysis(promptcontext.FileAnalysis{
		File:              conflictData.FileName,
		RepositorySummary: fmt.Sprintf("Repository root: %s", repoRoot),
		PromptContext:     promptContext,
		BaseAST:           baseASTData,
		OurAST:            ourASTData,
		TheirAST:          theirASTData,
		SmartDiff:         smartDiff,
	})

	if jsonBytes != nil {
		// Store report JSON in in-memory cache rather than writing to disk.
		cacheKey := cache.SaveReport(repoRoot, conflictData.FileName, jsonBytes)
		if cacheKey == "" {
			fmt.Fprintf(&buf, "Warning: failed to cache report for %s\n", conflictData.FileName)
		} else {
			reportPath = cacheKey
		}
	}

	if runAI {
		debugPrintf("[resolve] file %s: starting AI resolution for %d collision(s) via %s\n", targetFile, len(smartDiff.Collisions), cfg.Provider)
		fmt.Fprintf(&buf, "\nInitializing AI Provider: %s...\n", cfg.Provider)

		aiConfig := ai.AIConfig{
			Provider: cfg.Provider,
			Model:    cfg.Model,
			APIKey:   cfg.APIKey,
			BaseURL:  cfg.BaseURL,
		}

		resolver, err := ai.GetResolver(aiConfig)
		if err != nil {
			debugPrintf("[resolve] file %s: AI provider initialization failed in %s: %v\n", targetFile, formatDuration(time.Since(start)), err)
			fmt.Fprintf(&buf, "Failed to initialize AI provider: %v\n", err)
		} else {
			ctx := context.Background()
			for i, collision := range smartDiff.Collisions {
				collisionStart := time.Now()
				debugPrintf("[resolve] file %s: collision %d/%d [%s] %s (line %d) starting\n", targetFile, i+1, len(smartDiff.Collisions), collision.Kind, collision.Name, collision.Line)
				fmt.Fprintf(&buf, "  -> Requesting AI resolution for [%s] %s (Line %d)...\n", collision.Kind, collision.Name, collision.Line)

				res, aiErr := resolver.ResolveCollision(ctx, collision, promptContext)
				if aiErr != nil {
					debugPrintf("[resolve] file %s: collision %d/%d failed after %s: %v\n", targetFile, i+1, len(smartDiff.Collisions), formatDuration(time.Since(collisionStart)), aiErr)
					fmt.Fprintf(&buf, "     ⚠ AI Error: %v\n", aiErr)
					continue
				}

				debugPrintf("[resolve] file %s: collision %d/%d resolved in %s (confidence=%d%%)\n", targetFile, i+1, len(smartDiff.Collisions), formatDuration(time.Since(collisionStart)), res.Confidence)
				fmt.Fprintf(&buf, "     ✓ Resolved (Confidence: %d%%)\n", res.Confidence)
				fmt.Fprintf(&buf, "       Explanation: %s\n", res.Explanation)
				fmt.Fprintf(&buf, "       Suggested Code:\n")

				for _, line := range strings.Split(res.SuggestedCode, "\n") {
					fmt.Fprintf(&buf, "         %s\n", line)
				}
			}
		}
	}

	debugPrintf("[resolve] file %s: finished in %s\n", targetFile, formatDuration(time.Since(start)))

	// After AI resolution (if executed), remove the cached AST/report JSON to free memory.
	if runAI && reportPath != "" {
		cache.DeleteReport(repoRoot, conflictData.FileName)
		// indicate that the report is no longer stored
		reportPath = ""
		debugPrintf("[resolve] file %s: deleted cached report for %s\n", targetFile, conflictData.FileName)
	}

	return FileOutcome{Output: buf.String(), ReportPath: reportPath}
}
