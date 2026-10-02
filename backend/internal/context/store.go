// Package promptcontext builds and describes the prompt context for a
// conflicted file. Storage of these values is run-scoped and lives in the
// runstate package.
package promptcontext

import (
	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

// FileAnalysis bundles everything the API can expose about one analyzed file.
type FileAnalysis struct {
	File              string                   `json:"file"`
	RepositorySummary string                   `json:"repositorySummary"`
	PromptContext     PromptContextIR          `json:"promptContext"`
	BaseAST           parser.ASTContext        `json:"baseAst"`
	OurAST            parser.ASTContext        `json:"ourAst"`
	TheirAST          parser.ASTContext        `json:"theirAst"`
	SmartDiff         semantic.SmartDiffResult `json:"smartDiff"`
}
