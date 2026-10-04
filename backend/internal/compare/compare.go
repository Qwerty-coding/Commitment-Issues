package compare

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"CommitIssues/internal/cache"
	"CommitIssues/internal/fileutil"
	"CommitIssues/internal/git"
	"CommitIssues/internal/parser"
	"CommitIssues/internal/semantic"
)

// FileStatus classifies the nature of change or conflict between Base, Ours, and Theirs.
type FileStatus string

const (
	StatusOursOnly            FileStatus = "ours_only"
	StatusTheirsOnly          FileStatus = "theirs_only"
	StatusIdentical           FileStatus = "identical"
	StatusStructuralCollision FileStatus = "structural_collision"
	StatusContentConflict     FileStatus = "content_conflict"
	StatusAddAdd              FileStatus = "add_add_conflict"
	StatusDeleteModify        FileStatus = "delete_modify_conflict"
	StatusRenameConflict      FileStatus = "rename_conflict"
	StatusBinaryConflict      FileStatus = "binary_conflict"
	StatusUnsupported         FileStatus = "unsupported_language"
	// StatusError marks a file whose analysis failed (git read failure or
	// parse failure). It is never a silent "clean" result.
	StatusError FileStatus = "analysis_error"
)

// Options configures a commit or repository comparison.
type Options struct {
	Repo1     string `json:"repo1"`
	Repo2     string `json:"repo2,omitempty"`
	BaseRef   string `json:"baseRef,omitempty"`
	OursRef   string `json:"oursRef"`
	TheirsRef string `json:"theirsRef"`
	// Cache is the AST cache used for incremental parsing. When nil, a
	// per-comparison in-memory cache is created; cache state is never shared
	// across comparisons unless the caller explicitly injects one.
	Cache cache.Cache `json:"-"`
}

// FileComparison details the 3-way status and semantic diff for one file.
type FileComparison struct {
	Path           string                    `json:"path"`
	// OldPath is the base-side path when the file was renamed on a side.
	OldPath        string                    `json:"oldPath,omitempty"`
	Status         FileStatus                `json:"status"`
	Explanation    string                    `json:"explanation"`
	Recommendation string                    `json:"recommendation"`
	BaseHash       string                    `json:"baseHash,omitempty"`
	OursHash       string                    `json:"oursHash,omitempty"`
	TheirsHash     string                    `json:"theirsHash,omitempty"`
	IsBinary       bool                      `json:"isBinary"`
	IsSupported    bool                      `json:"isSupported"`
	SmartDiff      *semantic.SmartDiffResult `json:"smartDiff,omitempty"`
}

// Result is the deterministic outcome of a 3-way commit or repository comparison.
type Result struct {
	Repo1                 string           `json:"repo1"`
	Repo2                 string           `json:"repo2,omitempty"`
	BaseRef               string           `json:"baseRef,omitempty"`
	OursRef               string           `json:"oursRef"`
	TheirsRef             string           `json:"theirsRef"`
	MergeBaseResolved     bool             `json:"mergeBaseResolved"`
	UnrelatedRepositories bool             `json:"unrelatedRepositories"`
	Files                 []FileComparison `json:"files"`
	Summary               Summary          `json:"summary"`
}

// Summary aggregates counts by file status.
type Summary struct {
	TotalFiles            int `json:"totalFiles"`
	OursOnly              int `json:"oursOnly"`
	TheirsOnly            int `json:"theirsOnly"`
	Identical             int `json:"identical"`
	StructuralCollisions  int `json:"structuralCollisions"`
	ContentConflicts      int `json:"contentConflicts"`
	AddAddConflicts       int `json:"addAddConflicts"`
	DeleteModifyConflicts int `json:"deleteModifyConflicts"`
	RenameConflicts       int `json:"renameConflicts"`
	BinaryConflicts       int `json:"binaryConflicts"`
	Unsupported           int `json:"unsupported"`
	Errors                int `json:"errors"`
}

// fileVersions maps one logical file to its concrete path on each side.
// Paths differ across sides when a rename happened between base and a branch.
type fileVersions struct {
	displayPath     string
	basePath        string
	oursPath        string
	theirsPath      string
	renamedOnOurs   bool
	renamedOnTheirs bool
	// divergentRename is set when both sides renamed the same base file to
	// different targets (a rename/rename conflict).
	divergentRename bool
}

// CompareCommits executes hypothetical conflict analysis across commits or repositories
// without modifying any working-tree files.
func CompareCommits(ctx context.Context, opts Options) (*Result, error) {
	if opts.Repo1 == "" {
		return nil, fmt.Errorf("repo1 is required")
	}
	repo1 := opts.Repo1
	repo2 := opts.Repo2
	if repo2 == "" {
		repo2 = repo1
	}

	astCache := opts.Cache
	if astCache == nil {
		// Honour CACHE_* environment configuration; fall back to memory.
		if envCache, err := cache.NewFromEnv(); err == nil {
			astCache = envCache
		} else {
			astCache = cache.NewDefault()
		}
	}

	// 1. Resolve commit SHAs
	oursSha, err := git.ResolveRef(ctx, repo1, opts.OursRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ours ref %q: %w", opts.OursRef, err)
	}

	theirsSha, err := git.ResolveRef(ctx, repo2, opts.TheirsRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve theirs ref %q: %w", opts.TheirsRef, err)
	}

	// 2. Resolve merge base
	var baseSha string
	mergeBaseResolved := false
	unrelated := false

	if opts.BaseRef != "" {
		baseSha, err = git.ResolveRef(ctx, repo1, opts.BaseRef)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve base ref %q: %w", opts.BaseRef, err)
		}
		mergeBaseResolved = true
	} else if repo1 == repo2 {
		baseSha, err = git.FindMergeBase(ctx, repo1, oursSha, theirsSha)
		if err != nil {
			if errors.Is(err, git.ErrNoMergeBase) {
				unrelated = true
			} else {
				return nil, fmt.Errorf("failed to resolve merge base for %s and %s: %w", opts.OursRef, opts.TheirsRef, err)
			}
		} else {
			mergeBaseResolved = true
		}
	} else {
		shared, base, sharedErr := git.CheckRepositoriesSharedHistory(ctx, repo1, repo2, oursSha, theirsSha)
		if sharedErr != nil {
			return nil, fmt.Errorf("failed to check shared history: %w", sharedErr)
		}
		if shared && base != "" {
			baseSha = base
			mergeBaseResolved = true
		} else {
			unrelated = true
		}
	}

	// 3. Enumerate all changed files across commits
	fileSet := make(map[string]struct{})

	var renamesOurs, renamesTheirs []git.RenameInfo
	if mergeBaseResolved && baseSha != "" {
		diffOurs, diffErr := git.DiffTrees(ctx, repo1, baseSha, oursSha)
		if diffErr != nil {
			return nil, fmt.Errorf("failed to diff base..ours: %w", diffErr)
		}
		for _, f := range diffOurs {
			fileSet[f] = struct{}{}
		}
		diffTheirs, diffErr := git.DiffTrees(ctx, repo2, baseSha, theirsSha)
		if diffErr != nil {
			return nil, fmt.Errorf("failed to diff base..theirs: %w", diffErr)
		}
		for _, f := range diffTheirs {
			fileSet[f] = struct{}{}
		}

		renamesOurs, diffErr = git.DetectRenames(ctx, repo1, baseSha, oursSha)
		if diffErr != nil {
			return nil, fmt.Errorf("failed to detect renames base..ours: %w", diffErr)
		}
		renamesTheirs, diffErr = git.DetectRenames(ctx, repo2, baseSha, theirsSha)
		if diffErr != nil {
			return nil, fmt.Errorf("failed to detect renames base..theirs: %w", diffErr)
		}
	} else {
		// Unrelated or missing base: compare entire file trees
		oursFiles, err := git.ListCommitFiles(ctx, repo1, oursSha)
		if err != nil {
			return nil, fmt.Errorf("failed to list files for ours commit: %w", err)
		}
		for _, f := range oursFiles {
			fileSet[f] = struct{}{}
		}

		theirsFiles, err := git.ListCommitFiles(ctx, repo2, theirsSha)
		if err != nil {
			return nil, fmt.Errorf("failed to list files for theirs commit: %w", err)
		}
		for _, f := range theirsFiles {
			fileSet[f] = struct{}{}
		}
	}

	// Deterministic alphabetical ordering
	var allFiles []string
	for f := range fileSet {
		allFiles = append(allFiles, f)
	}
	sort.Strings(allFiles)

	logicalFiles := buildLogicalFiles(allFiles, renamesOurs, renamesTheirs)

	result := &Result{
		Repo1:                 repo1,
		Repo2:                 opts.Repo2,
		BaseRef:               baseSha,
		OursRef:               oursSha,
		TheirsRef:             theirsSha,
		MergeBaseResolved:     mergeBaseResolved,
		UnrelatedRepositories: unrelated,
		Files:                 make([]FileComparison, 0, len(logicalFiles)),
	}

	// 4. Analyze each file without touching the working tree
	for _, fv := range logicalFiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		comp := compareFile(ctx, astCache, repo1, repo2, baseSha, oursSha, theirsSha, fv, mergeBaseResolved)
		result.Files = append(result.Files, comp)

		switch comp.Status {
		case StatusOursOnly:
			result.Summary.OursOnly++
		case StatusTheirsOnly:
			result.Summary.TheirsOnly++
		case StatusIdentical:
			result.Summary.Identical++
		case StatusStructuralCollision:
			result.Summary.StructuralCollisions++
		case StatusContentConflict:
			result.Summary.ContentConflicts++
		case StatusAddAdd:
			result.Summary.AddAddConflicts++
		case StatusDeleteModify:
			result.Summary.DeleteModifyConflicts++
		case StatusRenameConflict:
			result.Summary.RenameConflicts++
		case StatusBinaryConflict:
			result.Summary.BinaryConflicts++
		case StatusUnsupported:
			result.Summary.Unsupported++
		case StatusError:
			result.Summary.Errors++
		}
	}

	result.Summary.TotalFiles = len(result.Files)
	return result, nil
}

// buildLogicalFiles merges raw changed paths with rename information so that
// a renamed file is analyzed as ONE logical file whose path may differ across
// base/ours/theirs — never misreported as an unrelated delete+add pair.
func buildLogicalFiles(allFiles []string, renamesOurs, renamesTheirs []git.RenameInfo) []fileVersions {
	oursMap := make(map[string]string, len(renamesOurs))
	for _, r := range renamesOurs {
		oursMap[r.OldPath] = r.NewPath
	}
	theirsMap := make(map[string]string, len(renamesTheirs))
	for _, r := range renamesTheirs {
		theirsMap[r.OldPath] = r.NewPath
	}

	handled := make(map[string]struct{})
	renameSources := make(map[string]struct{}, len(oursMap)+len(theirsMap))
	for old := range oursMap {
		renameSources[old] = struct{}{}
	}
	for old := range theirsMap {
		renameSources[old] = struct{}{}
	}

	var entries []fileVersions
	for old := range renameSources {
		fv := fileVersions{basePath: old, oursPath: old, theirsPath: old}
		newO, okO := oursMap[old]
		newT, okT := theirsMap[old]
		if okO {
			fv.oursPath = newO
			fv.renamedOnOurs = true
			handled[newO] = struct{}{}
		}
		if okT {
			fv.theirsPath = newT
			fv.renamedOnTheirs = true
			handled[newT] = struct{}{}
		}
		switch {
		case okO && okT && newO != newT:
			fv.divergentRename = true
			fv.displayPath = old
		case fv.renamedOnOurs:
			fv.displayPath = fv.oursPath
		case fv.renamedOnTheirs:
			fv.displayPath = fv.theirsPath
		default:
			fv.displayPath = old
		}
		handled[old] = struct{}{}
		entries = append(entries, fv)
	}

	for _, p := range allFiles {
		if _, consumed := handled[p]; consumed {
			continue
		}
		entries = append(entries, fileVersions{
			displayPath: p,
			basePath:    p,
			oursPath:    p,
			theirsPath:  p,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].displayPath < entries[j].displayPath })
	return entries
}

// fileError builds an explicit analysis_error entry. Comparison consumers can
// distinguish failed analysis from a genuine "clean merge" result.
func fileError(fv fileVersions, format string, args ...interface{}) FileComparison {
	return FileComparison{
		Path:           fv.displayPath,
		Status:         StatusError,
		Explanation:    fmt.Sprintf(format, args...),
		Recommendation: "Re-run the comparison; if the error persists, inspect the repository objects for this file",
	}
}

func compareFile(ctx context.Context, astCache cache.Cache, repo1, repo2, baseSha, oursSha, theirsSha string, fv fileVersions, hasBase bool) FileComparison {
	comp := FileComparison{
		Path:        fv.displayPath,
		IsSupported: parser.IsSupportedLanguage(fv.displayPath),
	}
	if fv.renamedOnOurs || fv.renamedOnTheirs {
		comp.OldPath = fv.basePath
	}

	// Rename/rename divergence: both sides renamed the same base file to
	// different targets. This is a conflict by definition; content AST
	// analysis across different paths would be misleading.
	if fv.divergentRename {
		comp.Status = StatusRenameConflict
		comp.Explanation = fmt.Sprintf("File %s renamed to %s on ours but to %s on theirs", fv.basePath, fv.oursPath, fv.theirsPath)
		comp.Recommendation = "Conflict: agree on a single target path, then reconcile the file contents"
		return comp
	}

	// Get blob hashes (fast check for equality or existence). Absence is
	// expected (add/delete); unexpected git failures become analysis errors.
	oursHash, errOurs := git.FileHashAtCommit(ctx, repo1, oursSha, fv.oursPath)
	if errOurs != nil && !errors.Is(errOurs, git.ErrFileNotInTree) {
		return fileError(fv, "failed to read ours blob for %s: %v", fv.oursPath, errOurs)
	}
	theirsHash, errTheirs := git.FileHashAtCommit(ctx, repo2, theirsSha, fv.theirsPath)
	if errTheirs != nil && !errors.Is(errTheirs, git.ErrFileNotInTree) {
		return fileError(fv, "failed to read theirs blob for %s: %v", fv.theirsPath, errTheirs)
	}
	var baseHash string
	if hasBase && baseSha != "" {
		var errBase error
		baseHash, errBase = git.FileHashAtCommit(ctx, repo1, baseSha, fv.basePath)
		if errBase != nil && !errors.Is(errBase, git.ErrFileNotInTree) {
			return fileError(fv, "failed to read base blob for %s: %v", fv.basePath, errBase)
		}
	}

	comp.OursHash = oursHash
	comp.TheirsHash = theirsHash
	comp.BaseHash = baseHash

	inOurs := oursHash != ""
	inTheirs := theirsHash != ""
	inBase := baseHash != ""

	renameNote := ""
	if fv.renamedOnOurs && fv.renamedOnTheirs {
		renameNote = fmt.Sprintf(" (both sides renamed %s to %s)", fv.basePath, fv.oursPath)
	} else if fv.renamedOnOurs {
		renameNote = fmt.Sprintf(" (ours renamed %s to %s)", fv.basePath, fv.oursPath)
	} else if fv.renamedOnTheirs {
		renameNote = fmt.Sprintf(" (theirs renamed %s to %s)", fv.basePath, fv.theirsPath)
	}

	// 1. Unchanged on one or both sides relative to Base
	if inBase && inOurs && inTheirs {
		if oursHash == theirsHash {
			comp.Status = StatusIdentical
			comp.Explanation = "Both branches made identical changes to the file" + renameNote
			comp.Recommendation = "Clean merge: identical changes can be combined automatically"
			return comp
		}
		if oursHash == baseHash {
			comp.Status = StatusTheirsOnly
			comp.Explanation = "File modified only on theirs branch" + renameNote
			comp.Recommendation = "Clean merge: adopt changes from theirs"
			return comp
		}
		if theirsHash == baseHash {
			comp.Status = StatusOursOnly
			comp.Explanation = "File modified only on ours branch" + renameNote
			comp.Recommendation = "Clean merge: retain changes from ours"
			return comp
		}
	}

	// 2. Add / Add (added on both sides without common base)
	if !inBase && inOurs && inTheirs {
		if oursHash == theirsHash {
			comp.Status = StatusIdentical
			comp.Explanation = "Identical file added on both sides"
			comp.Recommendation = "Clean merge: identical additions can be combined"
			return comp
		}
		comp.Status = StatusAddAdd
		comp.Explanation = "File added independently on both branches with different content"
		comp.Recommendation = "Resolve addition conflict: decide unified file content"
	}

	// 3. Rename / Delete conflicts (checked before delete/modify: a rename on
	// one side plus a delete on the other is not a plain delete/modify).
	if inBase && inOurs && !inTheirs && fv.renamedOnOurs {
		comp.Status = StatusRenameConflict
		comp.Explanation = fmt.Sprintf("File renamed to %s on ours, but deleted on theirs", fv.oursPath)
		comp.Recommendation = "Conflict: decide whether to keep the renamed file or delete it"
		return comp
	}
	if inBase && !inOurs && inTheirs && fv.renamedOnTheirs {
		comp.Status = StatusRenameConflict
		comp.Explanation = fmt.Sprintf("File renamed to %s on theirs, but deleted on ours", fv.theirsPath)
		comp.Recommendation = "Conflict: decide whether to keep the renamed file or delete it"
		return comp
	}

	// 4. Delete / Modify conflicts
	if inBase && !inOurs && inTheirs {
		if theirsHash == baseHash {
			comp.Status = StatusOursOnly
			comp.Explanation = "File deleted on ours; untouched on theirs"
			comp.Recommendation = "Clean merge: delete file"
			return comp
		}
		comp.Status = StatusDeleteModify
		comp.Explanation = "File deleted on ours branch, but modified on theirs branch"
		comp.Recommendation = "Conflict: decide whether to keep theirs modified version or delete file"
		return comp
	}
	if inBase && inOurs && !inTheirs {
		if oursHash == baseHash {
			comp.Status = StatusTheirsOnly
			comp.Explanation = "File deleted on theirs; untouched on ours"
			comp.Recommendation = "Clean merge: delete file"
			return comp
		}
		comp.Status = StatusDeleteModify
		comp.Explanation = "File modified on ours branch, but deleted on theirs branch"
		comp.Recommendation = "Conflict: decide whether to keep ours modified version or delete file"
		return comp
	}

	// 5. Single-sided additions / deletions
	if !inBase && inOurs && !inTheirs {
		comp.Status = StatusOursOnly
		comp.Explanation = "File added only on ours branch"
		comp.Recommendation = "Clean merge: retain ours addition"
		return comp
	}
	if !inBase && !inOurs && inTheirs {
		comp.Status = StatusTheirsOnly
		comp.Explanation = "File added only on theirs branch"
		comp.Recommendation = "Clean merge: adopt theirs addition"
		return comp
	}

	// 6. Read file contents from git commit objects (read-only)
	var oursBytes, theirsBytes, baseBytes []byte
	var err error
	if inOurs {
		if oursBytes, err = git.ShowFileAtCommit(ctx, repo1, oursSha, fv.oursPath); err != nil {
			return fileError(fv, "failed to read ours content for %s: %v", fv.oursPath, err)
		}
	}
	if inTheirs {
		if theirsBytes, err = git.ShowFileAtCommit(ctx, repo2, theirsSha, fv.theirsPath); err != nil {
			return fileError(fv, "failed to read theirs content for %s: %v", fv.theirsPath, err)
		}
	}
	if inBase {
		if baseBytes, err = git.ShowFileAtCommit(ctx, repo1, baseSha, fv.basePath); err != nil {
			return fileError(fv, "failed to read base content for %s: %v", fv.basePath, err)
		}
	}

	// Check binary content
	if fileutil.IsBinaryContent(oursBytes) || fileutil.IsBinaryContent(theirsBytes) || fileutil.IsBinaryContent(baseBytes) {
		comp.IsBinary = true
		comp.Status = StatusBinaryConflict
		comp.Explanation = "Binary file modified differently on both branches"
		comp.Recommendation = "Manual resolution required: choose either ours or theirs binary version"
		return comp
	}

	// Check unsupported language
	if !comp.IsSupported {
		comp.Status = StatusUnsupported
		comp.Explanation = fmt.Sprintf("Unsupported language (%s); parsed with text fallback", filepath.Ext(fv.displayPath))
		comp.Recommendation = "Review text differences manually to reconcile changes"
		return comp
	}

	// 7. AST & Semantic smart diff analysis
	baseCtx, _, parseErr := astCache.GetOrParse(ctx, repo1, fv.basePath, baseBytes)
	if parseErr != nil && !errors.Is(parseErr, parser.ErrFileTooLarge) && !errors.Is(parseErr, parser.ErrBinaryFile) {
		return fileError(fv, "failed to parse base version of %s: %v", fv.basePath, parseErr)
	}
	oursCtx, _, parseErr := astCache.GetOrParse(ctx, repo1, fv.oursPath, oursBytes)
	if parseErr != nil && !errors.Is(parseErr, parser.ErrFileTooLarge) && !errors.Is(parseErr, parser.ErrBinaryFile) {
		return fileError(fv, "failed to parse ours version of %s: %v", fv.oursPath, parseErr)
	}
	theirsCtx, _, parseErr := astCache.GetOrParse(ctx, repo2, fv.theirsPath, theirsBytes)
	if parseErr != nil && !errors.Is(parseErr, parser.ErrFileTooLarge) && !errors.Is(parseErr, parser.ErrBinaryFile) {
		return fileError(fv, "failed to parse theirs version of %s: %v", fv.theirsPath, parseErr)
	}

	smartDiff := semantic.GenerateSmartDiff(ctx, baseCtx, oursCtx, theirsCtx)
	comp.SmartDiff = &smartDiff

	if len(smartDiff.Collisions) > 0 {
		comp.Status = StatusStructuralCollision
		var collisionNames []string
		for _, c := range smartDiff.Collisions {
			collisionNames = append(collisionNames, fmt.Sprintf("%s (%s)", c.Name, c.Kind))
		}
		comp.Explanation = fmt.Sprintf("%d structural collision(s) detected: %s%s", len(smartDiff.Collisions), strings.Join(collisionNames, ", "), renameNote)
		comp.Recommendation = "Reconcile conflicting definitions using AST merge suggestions"
	} else if comp.Status == "" {
		comp.Status = StatusContentConflict
		comp.Explanation = "Both branches modified the file without structural AST symbol collisions" + renameNote
		comp.Recommendation = "Text-level merge: combine independent modifications"
	} else {
		comp.Explanation += renameNote
	}

	return comp
}
