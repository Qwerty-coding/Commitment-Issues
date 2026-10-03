// Package promptcontext builds and describes the prompt context for a
// conflicted file. Storage of these values is run-scoped and lives in the
// runstate package.
package promptcontext

import (
	"time"

	git "CommitIssues/internal/git"
	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

// FileAnalysis bundles everything the API can expose about one analyzed file.
type FileAnalysis struct {
	// Repository is the canonical repository root the file belongs to. It keeps
	// state for identical relative paths in different repositories separate.
	Repository        string                   `json:"repository,omitempty"`
	File              string                   `json:"file"`
	RepositorySummary string                   `json:"repositorySummary"`
	PromptContext     PromptContextIR          `json:"promptContext"`
	BaseAST           parser.ASTContext        `json:"baseAst"`
	OurAST            parser.ASTContext        `json:"ourAst"`
	TheirAST          parser.ASTContext        `json:"theirAst"`
	SmartDiff         semantic.SmartDiffResult `json:"smartDiff"`

	// ── Phase 2 additive fields ───────────────────────────────────────────
	// ContentHash is the SHA-256 hex hash of the working-tree file at
	// analysis time — the actual on-disk file that will be patched, not a
	// Git index stage. Used by safety checks to detect stale files.
	ContentHash string `json:"contentHash,omitempty"`
	// ConflictRegions is the structured list of all inline conflict regions
	// parsed from the working-tree file at analysis time. Each suggestion
	// must reference one of these regions; the patch builder uses them to
	// locate the exact byte range to replace.
	ConflictRegions []git.ConflictRegion `json:"conflictRegions,omitempty"`
	// RegionContextHashes maps region index (as string) to the SHA-256 hex
	// hash of the region text plus configurable context lines. Safety checks
	// compare the stored hash against the re-read file to detect changes.
	RegionContextHashes map[string]string `json:"regionContextHashes,omitempty"`
	// ParserVersion records the parser/schema version that produced this
	// analysis so stale cached analyses from incompatible parser upgrades
	// can be detected and invalidated.
	ParserVersion string `json:"parserVersion,omitempty"`
	// AnalysisTimestamp is the UTC time at which this analysis snapshot was
	// captured. It is informational; staleness is detected via content hashes.
	AnalysisTimestamp time.Time `json:"analysisTimestamp,omitempty"`
}
