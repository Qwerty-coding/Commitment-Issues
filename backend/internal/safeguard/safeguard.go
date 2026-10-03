// Package safeguard implements pre-apply safety checks mandated by Phase 4:
//
//   - Repository root canonicalization and identity verification.
//   - File path validation (path traversal, Windows drive/path escapes).
//   - Working-tree content and conflict-region context hash comparison.
//   - Suggestion revision and resolution status verification.
//
// All functions return typed *SafeError values carrying a stable ErrorCode so
// the API layer can return exact, machine-readable codes to the frontend.
package safeguard

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
	"CommitIssues/internal/resolutions"
)

// SafeError is a typed error carrying a stable, API-level error code.
type SafeError struct {
	Code    resolutions.ErrorCode
	Message string
}

func (e *SafeError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func safeErr(code resolutions.ErrorCode, format string, args ...interface{}) *SafeError {
	return &SafeError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// VerifyRepository canonicalizes repoRoot and verifies it is a real directory.
// Returns CodeRepositoryMismatch when the path cannot be resolved or does not
// exist.
func VerifyRepository(repoRoot string) (string, error) {
	if repoRoot == "" {
		return "", safeErr(resolutions.CodeRepositoryMismatch, "repository root is empty")
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", safeErr(resolutions.CodeRepositoryMismatch,
			"cannot canonicalize repository root %q: %v", repoRoot, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", safeErr(resolutions.CodeRepositoryMismatch,
			"repository root %q does not exist: %v", abs, err)
	}
	if !info.IsDir() {
		return "", safeErr(resolutions.CodeRepositoryMismatch,
			"repository root %q is not a directory", abs)
	}
	return abs, nil
}

// VerifyRepositoryIdentity checks that the stored repository root (from the
// resolution) matches the current canonical root provided by the caller.
// Returns CodeRepositoryMismatch on mismatch.
func VerifyRepositoryIdentity(storedRoot, currentRoot string) error {
	// Canonicalize both sides for a fair comparison.
	a, _ := filepath.Abs(storedRoot)
	b, _ := filepath.Abs(currentRoot)
	// Normalize separators on Windows so "C:\foo" == "C:/foo".
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a != b {
		return safeErr(resolutions.CodeRepositoryMismatch,
			"stored repository %q does not match current repository %q", storedRoot, currentRoot)
	}
	return nil
}

// VerifyFilePath verifies that relFile is a safe, repository-relative path
// that does not escape repoRoot. It rejects:
//
//   - Empty paths
//   - Paths containing NUL bytes
//   - Absolute paths (on all OSes)
//   - Paths starting with ".." components (path traversal)
//   - Windows drive letters (C:, D:, etc.) inside relative paths
//   - Paths that after Join+Clean escape the repository root
//
// Returns the absolute file path on success. Returns CodePathEscape on any
// violation.
func VerifyFilePath(repoRoot, relFile string) (string, error) {
	if relFile == "" {
		return "", safeErr(resolutions.CodePathEscape, "file path is empty")
	}
	if strings.ContainsAny(relFile, "\x00") {
		return "", safeErr(resolutions.CodePathEscape, "file path contains NUL byte")
	}

	// Reject absolute paths.
	if filepath.IsAbs(relFile) {
		return "", safeErr(resolutions.CodePathEscape,
			"file path %q must be relative, not absolute", relFile)
	}

	// Reject Windows drive letters embedded in the relative path (e.g. "C:foo").
	if runtime.GOOS == "windows" || looksLikeWindowsDrive(relFile) {
		if looksLikeWindowsDrive(relFile) {
			return "", safeErr(resolutions.CodePathEscape,
				"file path %q contains a Windows drive letter", relFile)
		}
	}

	// Reject paths that start with ".." after cleaning.
	cleaned := filepath.Clean(relFile)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", safeErr(resolutions.CodePathEscape,
			"file path %q escapes the repository root via path traversal", relFile)
	}

	// Final safety net: confirm the joined path stays inside the repository.
	absPath, err := fileutil.AbsFilePath(repoRoot, relFile)
	if err != nil {
		return "", safeErr(resolutions.CodePathEscape, "%v", err)
	}
	return absPath, nil
}

// looksLikeWindowsDrive returns true when s starts with a letter followed by
// a colon, regardless of the current OS.
func looksLikeWindowsDrive(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	return s[1] == ':' && ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'))
}

// WorkingTreeCheck is the result of a working-tree verification pass.
type WorkingTreeCheck struct {
	// CurrentData is the working-tree file bytes, already read.
	CurrentData []byte
	// CurrentHash is the SHA-256 hex hash of CurrentData.
	CurrentHash string
	// Regions are the conflict regions currently in the file.
	Regions []git.ConflictRegion
}

// VerifyWorkingTree re-reads the working-tree file and verifies it against the
// stored analysis snapshot. It checks:
//
//  1. The file exists and is readable.
//  2. The current content hash matches the stored analysis ContentHash.
//  3. The selected region still exists (by RegionID = fmt.Sprintf("%d", idx)).
//  4. The region context hash still matches the stored RegionContextHashes entry.
//
// Returns CodeStaleFile when any check fails. A successful return means the
// patch builder can proceed with the returned WorkingTreeCheck.
func VerifyWorkingTree(absPath string, analysis promptcontext.FileAnalysis, regionID string) (WorkingTreeCheck, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return WorkingTreeCheck{}, safeErr(resolutions.CodeStaleFile,
			"cannot read working-tree file %s: %v", absPath, err)
	}
	currentHash := fileutil.HashContent(data)

	// 2. Content hash check.
	if analysis.ContentHash != "" && currentHash != analysis.ContentHash {
		return WorkingTreeCheck{}, safeErr(resolutions.CodeStaleFile,
			"file %s changed since analysis (expected hash %s, got %s)",
			absPath, analysis.ContentHash, currentHash)
	}

	// 3. Re-parse regions from the current content.
	content := string(data)
	parsed := git.ParseConflictRegions(content)
	regions := parsed.Regions

	// Verify the selected region still exists.
	found := false
	for _, r := range regions {
		if fmt.Sprintf("%d", r.Index) == regionID {
			found = true
			break
		}
	}
	if !found {
		return WorkingTreeCheck{}, safeErr(resolutions.CodeStaleFile,
			"conflict region %s no longer exists in %s", regionID, absPath)
	}

	// 4. Context hash check.
	if storedCtxHash, ok := analysis.RegionContextHashes[regionID]; ok && storedCtxHash != "" {
		allLines := strings.Split(content, "\n")
		if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
			allLines = allLines[:len(allLines)-1]
		}
		// Locate the exact region for context hashing.
		for _, r := range regions {
			if fmt.Sprintf("%d", r.Index) == regionID {
				currentCtxHash := fileutil.RegionContextHash(allLines, r)
				if currentCtxHash != storedCtxHash {
					return WorkingTreeCheck{}, safeErr(resolutions.CodeStaleFile,
						"conflict region %s context changed in %s since analysis", regionID, absPath)
				}
				break
			}
		}
	}

	return WorkingTreeCheck{
		CurrentData: data,
		CurrentHash: currentHash,
		Regions:     regions,
	}, nil
}

// VerifyRevision checks that the resolution's stored suggestion revision
// matches the current suggestion revision. Returns CodeApprovalExpired when
// they differ (the suggestion was regenerated).
func VerifyRevision(storedRevision, currentRevision int) error {
	if storedRevision != currentRevision {
		return safeErr(resolutions.CodeApprovalExpired,
			"suggestion revision changed from %d to %d; re-approve before applying",
			storedRevision, currentRevision)
	}
	return nil
}

// VerifyApproval checks that the resolution carries an explicit, non-expired
// approval. Returns CodeNotApproved or CodeApprovalExpired as appropriate.
func VerifyApproval(res resolutions.Resolution) error {
	switch res.ApprovalStatus {
	case resolutions.ApprovalApproved:
		return nil
	case resolutions.ApprovalExpired:
		return safeErr(resolutions.CodeApprovalExpired,
			"approval for resolution %s has expired; re-approve before applying", res.ID)
	default:
		return safeErr(resolutions.CodeNotApproved,
			"resolution %s has not been approved; preview and approve before applying", res.ID)
	}
}

// VerifyStatus checks that the resolution is in a state that permits the
// requested operation. Returns CodeAlreadyApplied or CodeStaleFile as needed.
func VerifyStatus(res resolutions.Resolution) error {
	switch res.Status {
	case resolutions.StatusApplied:
		return safeErr(resolutions.CodeAlreadyApplied,
			"resolution %s is already applied", res.ID)
	case resolutions.StatusReverted:
		return safeErr(resolutions.CodeAlreadyApplied,
			"resolution %s was already applied and reverted", res.ID)
	case resolutions.StatusStale:
		return safeErr(resolutions.CodeStaleFile,
			"resolution %s is stale; re-analyze the file before applying", res.ID)
	case resolutions.StatusFailed:
		return safeErr(resolutions.CodeInvalidPatch,
			"resolution %s previously failed; check the error before retrying", res.ID)
	}
	return nil
}
