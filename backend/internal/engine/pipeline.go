package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ai "CommitIssues/internal/ai"
	promptcontext "CommitIssues/internal/context"
	git "CommitIssues/internal/git"
	graph "CommitIssues/internal/graph"
	parser "CommitIssues/internal/parser"
	prompt "CommitIssues/internal/prompt"
	report "CommitIssues/internal/report"
	semantic "CommitIssues/internal/semantic"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
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

func ResolveScanRoot(argPath string) string {
	if argPath != "" {
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

	conflictsByRepo := make(map[string][]string)
	for _, repoRoot := range repoRoots {
		conflictedFiles, err := git.GetConflictedFiles(repoRoot)
		if err == nil && len(conflictedFiles) > 0 {
			conflictsByRepo[repoRoot] = conflictedFiles
		}
	}
	return conflictsByRepo, repoRoots, nil
}

// ProcessRepository conflicts concurrently
func ProcessRepository(repoRoot string, conflictedFiles []string, cfg Config, runAI bool) []FileOutcome {
	outcomes := make([]FileOutcome, len(conflictedFiles))
	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.MaxConcurrency)

	for i, targetFile := range conflictedFiles {
		wg.Add(1)
		go func(idx int, file string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			outcomes[idx] = ProcessConflictFile(repoRoot, file, cfg, runAI)
		}(i, targetFile)
	}
	wg.Wait()
	return outcomes
}

// ProcessConflictFile runs AST extraction, Semantic Diff, Graphing, & optionally AI
func ProcessConflictFile(repoRoot, targetFile string, cfg Config, runAI bool) FileOutcome {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n--- Conflict: %s ---\n", targetFile)

	conflictData, err := git.ExtractConflictVersions(repoRoot, targetFile)
	if err != nil {
		return FileOutcome{Output: buf.String(), Err: fmt.Errorf("failed to extract versions: %w", err)}
	}
	fmt.Fprintln(&buf, "Successfully extracted Base, Ours, and Theirs code from Git index.")

	jsParser := sitter.NewParser()
	jsParser.SetLanguage(javascript.GetLanguage())

	ourSourceCode := parser.NormalizeUTF8([]byte(conflictData.OurVersion))
	ourTree, _ := jsParser.ParseCtx(context.Background(), nil, ourSourceCode)
	if ourTree == nil {
		return FileOutcome{Output: buf.String(), Err: fmt.Errorf("failed to parse OUR version into AST")}
	}

	theirSourceCode := parser.NormalizeUTF8([]byte(conflictData.TheirVersion))
	theirTree, _ := jsParser.ParseCtx(context.Background(), nil, theirSourceCode)
	if theirTree == nil {
		return FileOutcome{Output: buf.String(), Err: fmt.Errorf("failed to parse THEIR version into AST")}
	}

	baseSourceCode := parser.NormalizeUTF8([]byte(conflictData.BaseVersion))
	baseTree, _ := jsParser.ParseCtx(context.Background(), nil, baseSourceCode)

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
	if jsonBytes != nil {
		fullTokens := promptcontext.EstimateTokens(string(jsonBytes))
		promptcontext.RegisterPromptStatistics(promptcontext.PromptStatisticsDTO{
			File:                conflictData.FileName,
			EstimatedTokens:     promptContext.EstimatedTokens,
			FullPayloadTokens:   fullTokens,
			SelectedNodes:       len(conflictScope.Nodes),
			ExcludedNodes:       len(mergedSemanticGraph.Nodes) - len(conflictScope.Nodes),
			ReductionPercentage: promptcontext.BuildReductionPercentage(fullTokens, promptContext.EstimatedTokens),
		})

		path, saveErr := report.SaveReportFile(jsonBytes, repoRoot, conflictData.FileName)
		if saveErr != nil {
			fmt.Fprintf(&buf, "Warning: failed to save report file: %v\n", saveErr)
		} else {
			reportPath = path
		}
	}

	if runAI {
		fmt.Fprintf(&buf, "\nInitializing AI Provider: %s...\n", cfg.Provider)

		aiConfig := ai.AIConfig{
			Provider: cfg.Provider,
			Model:    cfg.Model,
			APIKey:   cfg.APIKey,
			BaseURL:  cfg.BaseURL,
		}

		resolver, err := ai.GetResolver(aiConfig)
		if err != nil {
			fmt.Fprintf(&buf, "Failed to initialize AI provider: %v\n", err)
		} else {
			ctx := context.Background()
			for _, collision := range smartDiff.Collisions {
				fmt.Fprintf(&buf, "  -> Requesting AI resolution for [%s] %s (Line %d)...\n", collision.Kind, collision.Name, collision.Line)

				res, aiErr := resolver.ResolveCollision(ctx, collision, promptContext)
				if aiErr != nil {
					fmt.Fprintf(&buf, "     ⚠ AI Error: %v\n", aiErr)
					continue
				}

				fmt.Fprintf(&buf, "     ✓ Resolved (Confidence: %d%%)\n", res.Confidence)
				fmt.Fprintf(&buf, "       Explanation: %s\n", res.Explanation)
				fmt.Fprintf(&buf, "       Suggested Code:\n")

				for _, line := range strings.Split(res.SuggestedCode, "\n") {
					fmt.Fprintf(&buf, "         %s\n", line)
				}
				
			}
		}
	}

	return FileOutcome{Output: buf.String(), ReportPath: reportPath}
}