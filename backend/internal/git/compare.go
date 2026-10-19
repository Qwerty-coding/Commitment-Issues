package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNoMergeBase indicates that two refs share no common ancestor commit.
// It is an expected outcome for unrelated histories, never a git failure.
var ErrNoMergeBase = errors.New("no common merge base")

// ErrFileNotInTree indicates that a path does not exist in a commit's tree.
// Absence is a first-class case (add/delete), distinct from git failures.
var ErrFileNotInTree = errors.New("file not present in commit tree")

// ResolveRef resolves a git reference (branch name, tag, HEAD, commit SHA)
// to a full 40-character commit SHA without mutating working tree.
func ResolveRef(ctx context.Context, repoRoot, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("ref cannot be empty")
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref+"^{commit}")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve ref %q in %s: %w", ref, repoRoot, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// FindMergeBase finds the common ancestor commit between two commit references.
// Unrelated histories yield ErrNoMergeBase; any other git failure is propagated.
func FindMergeBase(ctx context.Context, repoRoot, ref1, ref2 string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", ref1, ref2)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", fmt.Errorf("%w: %s and %s in %s", ErrNoMergeBase, ref1, ref2, repoRoot)
		}
		return "", fmt.Errorf("git merge-base %s %s failed in %s: %w", ref1, ref2, repoRoot, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ShowFileAtCommit reads the exact byte contents of a file at a specific commit
// from Git object database without touching the working tree. A path absent
// from the tree yields ErrFileNotInTree; git failures are propagated.
func ShowFileAtCommit(ctx context.Context, repoRoot, commitSha, relPath string) ([]byte, error) {
	cleanPath := strings.ReplaceAll(relPath, "\\", "/")
	cmd := exec.CommandContext(ctx, "git", "show", fmt.Sprintf("%s:%s", commitSha, cleanPath))
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
			return nil, fmt.Errorf("%w: %s at %s", ErrFileNotInTree, relPath, commitSha)
		}
		return nil, fmt.Errorf("failed to show %s at %s in %s: %w", relPath, commitSha, repoRoot, err)
	}
	return out, nil
}

// ListCommitFiles returns a sorted list of all file paths present at commitSha.
func ListCommitFiles(ctx context.Context, repoRoot, commitSha string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "--name-only", commitSha)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list files at %s in %s: %w", commitSha, repoRoot, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			files = append(files, trimmed)
		}
	}
	return files, nil
}

// DiffTrees returns a list of files modified, added, or deleted between commit1 and commit2.
func DiffTrees(ctx context.Context, repoRoot, commit1, commit2 string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", commit1, commit2)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to diff trees %s..%s in %s: %w", commit1, commit2, repoRoot, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			files = append(files, trimmed)
		}
	}
	return files, nil
}

// CheckRepositoriesSharedHistory tests whether repo1 and repo2 share commit ancestry.
// If repo1 == repo2, it looks up merge base directly.
// If different repositories, it checks whether commit ancestry can be resolved using
// GIT_ALTERNATE_OBJECT_DIRECTORIES without fetching or mutating either repository.
func CheckRepositoriesSharedHistory(ctx context.Context, repo1, repo2, ref1, ref2 string) (bool, string, error) {
	if repo1 == repo2 {
		base, err := FindMergeBase(ctx, repo1, ref1, ref2)
		if err != nil {
			if errors.Is(err, ErrNoMergeBase) {
				return false, "", nil
			}
			return false, "", err
		}
		return true, base, nil
	}

	// Cross-repo probes run with the other repository's object directory
	// visible, so a ref that only exists in one repository is reported as
	// "not shared" (merge-base exit 1) instead of a missing-object error.
	for _, probe := range []struct{ dir, altRepo string }{{repo1, repo2}, {repo2, repo1}} {
		altObj := getGitObjectsDir(ctx, probe.altRepo)
		if altObj == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, "git", "merge-base", ref1, ref2)
		cmd.Dir = probe.dir
		cmd.Env = append(os.Environ(), "GIT_ALTERNATE_OBJECT_DIRECTORIES="+altObj)
		out, err := cmd.Output()
		if err == nil {
			cand := strings.TrimSpace(string(out))
			if cand != "" {
				return true, cand, nil
			}
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, "", ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			continue // No common ancestor from this direction; try the other.
		}
		return false, "", fmt.Errorf("git merge-base %s %s failed in %s: %w", ref1, ref2, probe.dir, err)
	}

	// Unrelated repositories: no shared Git history
	return false, "", nil
}

func getGitObjectsDir(ctx context.Context, repoRoot string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-path", "objects")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		obj := filepath.Join(repoRoot, ".git", "objects")
		if _, statErr := os.Stat(obj); statErr == nil {
			return obj
		}
		return ""
	}
	p := strings.TrimSpace(string(out))
	if !filepath.IsAbs(p) {
		p = filepath.Join(repoRoot, p)
	}
	if _, statErr := os.Stat(p); statErr == nil {
		return p
	}
	return ""
}

// FileHashAtCommit returns the git blob sha1 hash of the file at commitSha.
func FileHashAtCommit(ctx context.Context, repoRoot, commitSha, relPath string) (string, error) {
	cleanPath := strings.ReplaceAll(relPath, "\\", "/")
	cmd := exec.CommandContext(ctx, "git", "ls-tree", commitSha, cleanPath)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("git ls-tree %s -- %s failed in %s: %w", commitSha, relPath, repoRoot, err)
	}
	parts := bytes.Fields(out)
	if len(parts) >= 3 {
		return string(parts[2]), nil
	}
	return "", fmt.Errorf("%w: %s at %s", ErrFileNotInTree, relPath, commitSha)
}

// RenameInfo represents a detected git file rename.
type RenameInfo struct {
	OldPath string
	NewPath string
	Score   int
}

// DetectRenames runs git diff -M --name-status to identify renamed files between two commits.
func DetectRenames(ctx context.Context, repoRoot, commit1, commit2 string) ([]RenameInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "-M", "--name-status", commit1, commit2)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to detect renames %s..%s in %s: %w", commit1, commit2, repoRoot, err)
	}

	var renames []RenameInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts := strings.Split(l, "\t")
		if len(parts) >= 3 && strings.HasPrefix(parts[0], "R") {
			var score int
			fmt.Sscanf(parts[0], "R%d", &score)
			renames = append(renames, RenameInfo{
				OldPath: parts[1],
				NewPath: parts[2],
				Score:   score,
			})
		}
	}
	return renames, nil
}
// GitRef is one selectable reference in a repository, surfaced to the compare
// tab's autocomplete so users never have to type a hash from memory.
type GitRef struct {
	// Name is the ref's short name (e.g. "main", "v1.2.0", "origin/main").
	Name string `json:"name"`
	// Type is one of "branch", "remote", "tag" or "head".
	Type string `json:"type"`
	// Commit is the resolved short SHA the ref currently points at.
	Commit string `json:"commit,omitempty"`
}

// ListRefs returns the repository's branches, remote branches and tags in a
// deterministic order, each with the short SHA it resolves to. It never
// mutates the repository. A non-git directory yields an empty list and no
// error so the API can degrade gracefully.
func ListRefs(ctx context.Context, repoRoot string) ([]GitRef, error) {
	refs := make([]GitRef, 0)

	// Local branches: "refs/heads/<name>".
	branches, err := listRefsByPrefix(ctx, repoRoot, "refs/heads", "branch")
	if err != nil {
		return nil, err
	}
	refs = append(refs, branches...)

	// Remote-tracking branches: "refs/remotes/<name>" (skips the symbolic
	// "<remote>/HEAD" entries git creates automatically).
	remotes, err := listRefsByPrefix(ctx, repoRoot, "refs/remotes", "remote")
	if err != nil {
		return nil, err
	}
	for _, r := range remotes {
		if strings.HasSuffix(r.Name, "/HEAD") {
			continue
		}
		refs = append(refs, r)
	}

	// Tags: "refs/tags/<name>".
	tags, err := listRefsByPrefix(ctx, repoRoot, "refs/tags", "tag")
	if err != nil {
		return nil, err
	}
	refs = append(refs, tags...)

	// The current HEAD as a convenience entry (detached HEAD included).
	if head, err := ResolveRef(ctx, repoRoot, "HEAD"); err == nil && head != "" {
		refs = append([]GitRef{{Name: "HEAD", Type: "head", Commit: shortSHA(head)}}, refs...)
	}

	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Type != refs[j].Type {
			return refs[i].Type < refs[j].Type
		}
		return refs[i].Name < refs[j].Name
	})
	return refs, nil
}

// listRefsByPrefix enumerates refs under a full ref prefix and resolves each
// to its commit SHA.
func listRefsByPrefix(ctx context.Context, repoRoot, prefix, kind string) ([]GitRef, error) {
	cmd := exec.CommandContext(ctx, "git", "for-each-ref", "--format=%(refname:short) %(objectname)", prefix)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// A repository with no refs of this kind is not an error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("git for-each-ref %s failed in %s: %w", prefix, repoRoot, err)
	}

	var refs []GitRef
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		refs = append(refs, GitRef{Name: parts[0], Type: kind, Commit: shortSHA(parts[1])})
	}
	return refs, nil
}

// shortSHA abbreviates a 40-character SHA to 12 characters for display.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}