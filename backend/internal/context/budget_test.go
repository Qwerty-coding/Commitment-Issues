package promptcontext

import (
	"strings"
	"testing"

	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

func TestApplyTargetBudget_DisabledAndUnderBudget(t *testing.T) {
	ir := PromptContextIR{Context: "small", EstimatedTokens: 10}

	if got := ApplyTargetBudget(ir, 0); got.Context != ir.Context {
		t.Error("target<=0 must disable budgeting")
	}
	if got := ApplyTargetBudget(ir, 100); got.Context != ir.Context {
		t.Error("under-budget context must be returned unchanged")
	}
}

// buildIR returns a real assembled IR: one README, one function and one var.
func buildIR(t *testing.T) PromptContextIR {
	t.Helper()
	fn := parser.CodeElement{Name: "calculate", Kind: "Function", Line: 1, File: "calc.go", Content: strings.Repeat("f", 200)}
	variable := parser.CodeElement{Name: "config", Kind: "Variable", Line: 20, File: "calc.go", Content: strings.Repeat("v", 200)}
	ast := parser.ASTContext{Functions: []parser.CodeElement{fn}, Variables: []parser.CodeElement{variable}}
	scope := semantic.SemanticGraph{Nodes: []semantic.SemanticNode{
		{Identity: semantic.SymbolIdentity(fn), Kind: "Function", Name: "calculate"},
		{Identity: semantic.SymbolIdentity(variable), Kind: "Variable", Name: "config"},
	}}
	return BuildPromptContext("Repository root: /repo", strings.Repeat("r", 4000), []string{"calc.go"}, scope, ast, parser.ASTContext{})
}

func TestApplyTargetBudget_TrimsReadmeFirst(t *testing.T) {
	ir := buildIR(t)
	if ir.RepositoryReadme == "" {
		t.Fatal("fixture should include a README")
	}
	// Force one token of trimming: the README is dropped first, after which the
	// small function/variable fit comfortably, so they are preserved.
	got := ApplyTargetBudget(ir, ir.EstimatedTokens-1)
	if got.RepositoryReadme != "" {
		t.Error("README excerpt should be trimmed first")
	}
	if len(got.Functions) != 1 {
		t.Errorf("functions should survive README-only trimming, got %d", len(got.Functions))
	}
	if len(got.Variables) != 1 {
		t.Errorf("variables should survive README-only trimming, got %d", len(got.Variables))
	}
	if !strings.Contains(got.Context, "calculate") {
		t.Error("rendered context should still contain the scoped function")
	}
}

func TestApplyTargetBudget_TrimsFunctionsThenVariables(t *testing.T) {
	ir := buildIR(t)
	// A tiny budget forces trimming all the way down; functions must go before
	// variables.
	got := ApplyTargetBudget(ir, 1)
	if got.RepositoryReadme != "" {
		t.Error("README should be trimmed")
	}
	if len(got.Functions) != 0 {
		t.Errorf("functions should be trimmed under an extreme budget, got %d", len(got.Functions))
	}
	if len(got.Variables) != 0 {
		t.Errorf("variables should be trimmed last under an extreme budget, got %d", len(got.Variables))
	}
}
