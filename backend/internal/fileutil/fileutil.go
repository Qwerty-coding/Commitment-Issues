// Package fileutil provides utilities for working-tree file operations used
// by Phase 3: content hashing, region-context hashing, and atomic writes.
package fileutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	git "CommitIssues/internal/git"
)

// ParserVersion is the current analysis schema version. Increment this when
// the FileAnalysis shape or the conflict parser output changes in a way that
// makes old snapshots incompatible.
const ParserVersion = "1"

// HashContent returns the hex-encoded SHA-256 hash of data.
func HashContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile reads the file at absPath and returns its SHA-256 hex hash.
func HashFile(absPath string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("fileutil: cannot hash %s: %w", absPath, err)
	}
	return HashContent(data), nil
}

// RegionContextLines is the number of lines of surrounding context included
// in the per-region context hash. Changing this value invalidates all stored
// context hashes.
const RegionContextLines = 3

// RegionContextHash computes the SHA-256 hash of a conflict region's text
// plus up to RegionContextLines lines of surrounding context from the
// working-tree file content. The hash includes the full region markers so it
// is sensitive to both the region content and its position in the file.
//
// allLines must be the working-tree file split on "\n" (no trailing empty
// element). Indices are 1-based (ConflictRegion.StartLine / EndLine).
func RegionContextHash(allLines []string, region git.ConflictRegion) string {
	// Clamp to valid 0-based slice indices.
	start := region.StartLine - 1 - RegionContextLines
	if start < 0 {
		start = 0
	}
	end := region.EndLine + RegionContextLines
	if end > len(allLines) {
		end = len(allLines)
	}

	h := sha256.New()
	for _, line := range allLines[start:end] {
		_, _ = fmt.Fprintln(h, line)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RegionContextHashesForFile computes per-region context hashes for all
// regions in the working-tree file at absPath, keyed by fmt.Sprintf("%d",
// region.Index). It also returns the raw file content hash.
//
// Returns an empty map (not nil) when no regions are found.
func RegionContextHashesForFile(absPath string) (contentHash string, regionHashes map[string]string, err error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", nil, fmt.Errorf("fileutil: cannot read %s: %w", absPath, err)
	}
	contentHash = HashContent(data)
	content := string(data)

	parsed := git.ParseConflictRegions(content)
	regionHashes = make(map[string]string, len(parsed.Regions))

	if len(parsed.Regions) == 0 {
		return contentHash, regionHashes, nil
	}

	// Build line slice (without trailing empty element from trailing newline).
	allLines := strings.Split(content, "\n")
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}

	for _, region := range parsed.Regions {
		key := fmt.Sprintf("%d", region.Index)
		regionHashes[key] = RegionContextHash(allLines, region)
	}
	return contentHash, regionHashes, nil
}

// AbsFilePath returns the absolute path of a repository-relative file,
// verifying it stays inside repoRoot. Returns an error for path traversal,
// Windows drive escapes, or other unsafe paths.
func AbsFilePath(repoRoot, relFile string) (string, error) {
	// Reject obviously unsafe inputs before Join.
	if strings.ContainsAny(relFile, "\x00") {
		return "", fmt.Errorf("fileutil: file path contains NUL byte")
	}

	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("fileutil: cannot resolve repo root %q: %w", repoRoot, err)
	}

	joined := filepath.Join(absRepo, relFile)
	cleaned := filepath.Clean(joined)

	// Ensure the result is still inside the repository root.
	if !strings.HasPrefix(cleaned, absRepo+string(filepath.Separator)) &&
		cleaned != absRepo {
		return "", fmt.Errorf("fileutil: path %q escapes repository root %q", relFile, absRepo)
	}

	return cleaned, nil
}

// AtomicWrite writes data to targetPath atomically using a temp file in the
// same directory, then renaming. This ensures either the old or new content
// is visible, never a partial write.
func AtomicWrite(targetPath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".commit-issues-tmp-*")
	if err != nil {
		return fmt.Errorf("fileutil: cannot create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Always attempt cleanup on failure.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fileutil: cannot write temp file %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fileutil: cannot chmod temp file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fileutil: cannot close temp file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("fileutil: cannot rename %s -> %s: %w", tmpName, targetPath, err)
	}
	success = true
	return nil
}
