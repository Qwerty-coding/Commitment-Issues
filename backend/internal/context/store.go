package promptcontext

import (
	"sync"

	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

// promptStore holds the most recently generated PromptContextIR per
// conflicted file. Mirrors internal/graph's graphStore pattern: the
// pipeline registers results as it computes them, REST handlers just read.
var (
	storeMu sync.Mutex
	store   = make(map[string]PromptContextIR)
	order   []string

	analysisMu    sync.Mutex
	analysisStore = make(map[string]FileAnalysis)
	analysisOrder []string
)

type FileAnalysis struct {
	File              string                   `json:"file"`
	RepositorySummary string                   `json:"repositorySummary"`
	PromptContext     PromptContextIR          `json:"promptContext"`
	PromptStatistics  PromptStatisticsDTO      `json:"promptStatistics"`
	BaseAST           parser.ASTContext        `json:"baseAst"`
	OurAST            parser.ASTContext        `json:"ourAst"`
	TheirAST          parser.ASTContext        `json:"theirAst"`
	SmartDiff         semantic.SmartDiffResult `json:"smartDiff"`
}

func RegisterPromptContext(fileName string, ctx PromptContextIR) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if _, exists := store[fileName]; !exists {
		order = append(order, fileName)
	}
	store[fileName] = ctx
}

func GetPromptContext(fileName string) (PromptContextIR, bool) {
	storeMu.Lock()
	defer storeMu.Unlock()
	ctx, ok := store[fileName]
	return ctx, ok
}

func AllPromptContexts() []PromptContextIR {
	storeMu.Lock()
	defer storeMu.Unlock()
	out := make([]PromptContextIR, 0, len(order))
	for _, f := range order {
		out = append(out, store[f])
	}
	return out
}

func RegisterAnalysis(analysis FileAnalysis) {
	analysisMu.Lock()
	defer analysisMu.Unlock()
	if _, exists := analysisStore[analysis.File]; !exists {
		analysisOrder = append(analysisOrder, analysis.File)
	}
	analysisStore[analysis.File] = analysis
}

func GetAnalysis(fileName string) (FileAnalysis, bool) {
	analysisMu.Lock()
	defer analysisMu.Unlock()
	analysis, ok := analysisStore[fileName]
	return analysis, ok
}

func AllAnalyses() []FileAnalysis {
	analysisMu.Lock()
	defer analysisMu.Unlock()
	out := make([]FileAnalysis, 0, len(analysisOrder))
	for _, f := range analysisOrder {
		out = append(out, analysisStore[f])
	}
	return out
}
