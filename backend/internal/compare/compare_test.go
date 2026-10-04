package compare_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"CommitIssues/internal/compare"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\nOutput: %s", args, dir, err, string(out))
	}
	return string(out)
}

func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "compare_test_repo_*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@commitissues.local")
	runGit(t, dir, "config", "user.name", "Test User")

	return dir
}

// 1. Same file modified differently -> structural collision detected
func TestCompare_SameFileModifiedDifferently_StructuralCollision(t *testing.T) {
	repo := setupTestGitRepo(t)

	// Base commit
	mainPath := filepath.Join(repo, "service.go")
	_ = os.WriteFile(mainPath, []byte("package main\n\nfunc ProcessData(x int) int {\n    return x\n}\n"), 0644)
	runGit(t, repo, "add", "service.go")
	runGit(t, repo, "commit", "-m", "base commit")

	// Branch ours
	runGit(t, repo, "checkout", "-b", "feature-ours")
	_ = os.WriteFile(mainPath, []byte("package main\n\nfunc ProcessData(x int) int {\n    return x + 10\n}\n"), 0644)
	runGit(t, repo, "commit", "-am", "ours modification")

	// Branch theirs
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "feature-theirs")
	_ = os.WriteFile(mainPath, []byte("package main\n\nfunc ProcessData(x int) int {\n    return x * 2\n}\n"), 0644)
	runGit(t, repo, "commit", "-am", "theirs modification")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "feature-ours",
		TheirsRef: "feature-theirs",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.StructuralCollisions != 1 {
		t.Fatalf("expected 1 structural collision, got %d", result.Summary.StructuralCollisions)
	}
	if len(result.Files) != 1 || result.Files[0].Status != compare.StatusStructuralCollision {
		t.Fatalf("unexpected file status: %+v", result.Files)
	}
}

// 2. Same change on both sides -> identical clean merge
func TestCompare_SameChangeOnBothSides_Identical(t *testing.T) {
	repo := setupTestGitRepo(t)

	mainPath := filepath.Join(repo, "calc.py")
	_ = os.WriteFile(mainPath, []byte("def add(a, b):\n    return a + b\n"), 0644)
	runGit(t, repo, "add", "calc.py")
	runGit(t, repo, "commit", "-m", "base")

	// Ours
	runGit(t, repo, "checkout", "-b", "ours-calc")
	_ = os.WriteFile(mainPath, []byte("def add(a, b):\n    # docstring\n    return a + b\n"), 0644)
	runGit(t, repo, "commit", "-am", "ours identical change")

	// Theirs
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "theirs-calc")
	_ = os.WriteFile(mainPath, []byte("def add(a, b):\n    # docstring\n    return a + b\n"), 0644)
	runGit(t, repo, "commit", "-am", "theirs identical change")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "ours-calc",
		TheirsRef: "theirs-calc",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.Identical != 1 {
		t.Fatalf("expected 1 identical change, got %d", result.Summary.Identical)
	}
	if result.Files[0].Status != compare.StatusIdentical {
		t.Fatalf("expected status identical, got %s", result.Files[0].Status)
	}
}

// 3. Ours-only and Theirs-only changes
func TestCompare_OursOnlyAndTheirsOnly(t *testing.T) {
	repo := setupTestGitRepo(t)

	fileA := filepath.Join(repo, "file_a.txt")
	fileB := filepath.Join(repo, "file_b.txt")
	_ = os.WriteFile(fileA, []byte("content A\n"), 0644)
	_ = os.WriteFile(fileB, []byte("content B\n"), 0644)
	runGit(t, repo, "add", "file_a.txt", "file_b.txt")
	runGit(t, repo, "commit", "-m", "base")

	// Ours modifies file A
	runGit(t, repo, "checkout", "-b", "branch-a")
	_ = os.WriteFile(fileA, []byte("modified A\n"), 0644)
	runGit(t, repo, "commit", "-am", "mod A")

	// Theirs modifies file B
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "branch-b")
	_ = os.WriteFile(fileB, []byte("modified B\n"), 0644)
	runGit(t, repo, "commit", "-am", "mod B")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "branch-a",
		TheirsRef: "branch-b",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.OursOnly != 1 || result.Summary.TheirsOnly != 1 {
		t.Fatalf("expected 1 ours_only and 1 theirs_only, got: %+v", result.Summary)
	}
}

// 4. Add / Add conflict
func TestCompare_AddAddConflict(t *testing.T) {
	repo := setupTestGitRepo(t)

	dummy := filepath.Join(repo, "README.md")
	_ = os.WriteFile(dummy, []byte("# Project\n"), 0644)
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	// Ours adds new_file.js
	runGit(t, repo, "checkout", "-b", "branch-add-ours")
	newFile := filepath.Join(repo, "new_file.js")
	_ = os.WriteFile(newFile, []byte("console.log('ours');\n"), 0644)
	runGit(t, repo, "add", "new_file.js")
	runGit(t, repo, "commit", "-m", "add ours")

	// Theirs adds new_file.js with different content
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "branch-add-theirs")
	_ = os.WriteFile(newFile, []byte("console.log('theirs');\n"), 0644)
	runGit(t, repo, "add", "new_file.js")
	runGit(t, repo, "commit", "-m", "add theirs")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "branch-add-ours",
		TheirsRef: "branch-add-theirs",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.AddAddConflicts != 1 {
		t.Fatalf("expected 1 add_add conflict, got %d", result.Summary.AddAddConflicts)
	}
}

// 5. Delete / Modify conflict
func TestCompare_DeleteModifyConflict(t *testing.T) {
	repo := setupTestGitRepo(t)

	target := filepath.Join(repo, "config.json")
	_ = os.WriteFile(target, []byte("{\"port\": 8080}\n"), 0644)
	runGit(t, repo, "add", "config.json")
	runGit(t, repo, "commit", "-m", "add config")

	// Ours deletes config.json
	runGit(t, repo, "checkout", "-b", "branch-del")
	_ = os.Remove(target)
	runGit(t, repo, "rm", "config.json")
	runGit(t, repo, "commit", "-m", "delete config")

	// Theirs modifies config.json
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "branch-mod")
	_ = os.WriteFile(target, []byte("{\"port\": 9090}\n"), 0644)
	runGit(t, repo, "commit", "-am", "modify config")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "branch-del",
		TheirsRef: "branch-mod",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.DeleteModifyConflicts != 1 {
		t.Fatalf("expected 1 delete_modify conflict, got %d", result.Summary.DeleteModifyConflicts)
	}
}

// 6. Binary file conflicts
func TestCompare_BinaryFiles(t *testing.T) {
	repo := setupTestGitRepo(t)

	binPath := filepath.Join(repo, "image.png")
	_ = os.WriteFile(binPath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00base"), 0644)
	runGit(t, repo, "add", "image.png")
	runGit(t, repo, "commit", "-m", "add base binary")

	// Ours
	runGit(t, repo, "checkout", "-b", "bin-ours")
	_ = os.WriteFile(binPath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00ours"), 0644)
	runGit(t, repo, "commit", "-am", "ours binary")

	// Theirs
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "bin-theirs")
	_ = os.WriteFile(binPath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00theirs"), 0644)
	runGit(t, repo, "commit", "-am", "theirs binary")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "bin-ours",
		TheirsRef: "bin-theirs",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.BinaryConflicts != 1 {
		t.Fatalf("expected 1 binary conflict, got %d", result.Summary.BinaryConflicts)
	}
}

// 7. Invalid commit reference surfaces structured error
func TestCompare_InvalidCommit(t *testing.T) {
	repo := setupTestGitRepo(t)

	_, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "non-existent-commit-sha",
		TheirsRef: "master",
	})
	if err == nil {
		t.Fatalf("expected error for invalid commit ref")
	}
}

// 8. Two unrelated repositories report UnrelatedRepositories: true
func TestCompare_TwoRepositories_Unrelated(t *testing.T) {
	repo1 := setupTestGitRepo(t)
	repo2 := setupTestGitRepo(t)

	f1 := filepath.Join(repo1, "a.txt")
	_ = os.WriteFile(f1, []byte("repo1 file\n"), 0644)
	runGit(t, repo1, "add", "a.txt")
	runGit(t, repo1, "commit", "-m", "repo1 init")

	f2 := filepath.Join(repo2, "b.txt")
	_ = os.WriteFile(f2, []byte("repo2 file\n"), 0644)
	runGit(t, repo2, "add", "b.txt")
	runGit(t, repo2, "commit", "-m", "repo2 init")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo1,
		Repo2:     repo2,
		OursRef:   "master",
		TheirsRef: "master",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed for unrelated repos: %v", err)
	}

	if !result.UnrelatedRepositories {
		t.Fatalf("expected UnrelatedRepositories: true")
	}
}

// 9. Rename on one side + in-place edit on the other is ONE logical file,
// never a misleading delete+add pair.
func TestCompare_RenameOnOneSide_SingleLogicalFile(t *testing.T) {
	repo := setupTestGitRepo(t)

	oldPath := filepath.Join(repo, "old.go")
	_ = os.WriteFile(oldPath, []byte("package main\n\nfunc Work() int {\n    return 1\n}\n"), 0644)
	runGit(t, repo, "add", "old.go")
	runGit(t, repo, "commit", "-m", "base")

	// Ours renames old.go -> new.go and edits it
	runGit(t, repo, "checkout", "-b", "rename-ours")
	runGit(t, repo, "mv", "old.go", "new.go")
	_ = os.WriteFile(filepath.Join(repo, "new.go"), []byte("package main\n\nfunc Work() int {\n    return 2\n}\n"), 0644)
	runGit(t, repo, "commit", "-am", "rename and edit")

	// Theirs edits old.go in place
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "edit-theirs")
	_ = os.WriteFile(oldPath, []byte("package main\n\nfunc Work() int {\n    return 3\n}\n"), 0644)
	runGit(t, repo, "commit", "-am", "edit in place")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "rename-ours",
		TheirsRef: "edit-theirs",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 logical file, got %d: %+v", len(result.Files), result.Files)
	}
	f := result.Files[0]
	if f.Status == compare.StatusDeleteModify || f.Status == compare.StatusAddAdd ||
		f.Status == compare.StatusOursOnly || f.Status == compare.StatusTheirsOnly {
		t.Fatalf("rename misreported as %s: %+v", f.Status, f)
	}
	if f.Path != "new.go" || f.OldPath != "old.go" {
		t.Fatalf("expected path new.go with oldPath old.go, got %+v", f)
	}
}

// 10. Divergent rename (same base file renamed to different targets) is a
// rename conflict, not a delete/add pair.
func TestCompare_DivergentRename_RenameConflict(t *testing.T) {
	repo := setupTestGitRepo(t)

	base := filepath.Join(repo, "util.py")
	_ = os.WriteFile(base, []byte("def helper():\n    return 1\n"), 0644)
	runGit(t, repo, "add", "util.py")
	runGit(t, repo, "commit", "-m", "base")

	runGit(t, repo, "checkout", "-b", "rename-a")
	runGit(t, repo, "mv", "util.py", "util_a.py")
	runGit(t, repo, "commit", "-m", "rename to util_a")

	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "rename-b")
	runGit(t, repo, "mv", "util.py", "util_b.py")
	runGit(t, repo, "commit", "-m", "rename to util_b")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "rename-a",
		TheirsRef: "rename-b",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.RenameConflicts != 1 {
		t.Fatalf("expected 1 rename conflict, got %d (files: %+v)", result.Summary.RenameConflicts, result.Files)
	}
	if len(result.Files) != 1 || result.Files[0].Status != compare.StatusRenameConflict {
		t.Fatalf("expected single rename_conflict entry, got %+v", result.Files)
	}
}

// 11. Rename on one side versus delete on the other is a rename conflict.
func TestCompare_RenameVersusDelete_RenameConflict(t *testing.T) {
	repo := setupTestGitRepo(t)

	target := filepath.Join(repo, "a.txt")
	_ = os.WriteFile(target, []byte("important content line one\nimportant content line two\n"), 0644)
	runGit(t, repo, "add", "a.txt")
	runGit(t, repo, "commit", "-m", "base")

	runGit(t, repo, "checkout", "-b", "rename-side")
	runGit(t, repo, "mv", "a.txt", "b.txt")
	runGit(t, repo, "commit", "-m", "rename a to b")

	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "delete-side")
	runGit(t, repo, "rm", "a.txt")
	runGit(t, repo, "commit", "-m", "delete a")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "rename-side",
		TheirsRef: "delete-side",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.RenameConflicts != 1 {
		t.Fatalf("expected 1 rename conflict, got %d (files: %+v)", result.Summary.RenameConflicts, result.Files)
	}
}

// 12. Unrelated branches inside ONE repository report no merge base without
// an error, via the ErrNoMergeBase classification.
func TestCompare_OrphanBranches_NoMergeBase(t *testing.T) {
	repo := setupTestGitRepo(t)

	_ = os.WriteFile(filepath.Join(repo, "one.txt"), []byte("one\n"), 0644)
	runGit(t, repo, "add", "one.txt")
	runGit(t, repo, "commit", "-m", "first history")

	runGit(t, repo, "checkout", "--orphan", "orphan-branch")
	runGit(t, repo, "rm", "-rf", ".")
	_ = os.WriteFile(filepath.Join(repo, "two.txt"), []byte("two\n"), 0644)
	runGit(t, repo, "add", "two.txt")
	runGit(t, repo, "commit", "-m", "second unrelated history")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "master",
		TheirsRef: "orphan-branch",
	})
	if err != nil {
		t.Fatalf("orphan comparison must not error, got: %v", err)
	}
	if result.MergeBaseResolved {
		t.Fatalf("expected no merge base for orphan histories")
	}
	if len(result.Files) == 0 {
		t.Fatalf("expected full-tree comparison results for unrelated histories")
	}
}

// 9. Rename / Rename conflict
func TestCompare_RenameConflict(t *testing.T) {
	repo := setupTestGitRepo(t)

	baseFile := filepath.Join(repo, "doc.txt")
	_ = os.WriteFile(baseFile, []byte("original document content\n"), 0644)
	runGit(t, repo, "add", "doc.txt")
	runGit(t, repo, "commit", "-m", "base commit")

	// Branch ours renames doc.txt -> doc_ours.txt
	runGit(t, repo, "checkout", "-b", "branch-rename-ours")
	runGit(t, repo, "mv", "doc.txt", "doc_ours.txt")
	runGit(t, repo, "commit", "-m", "rename to ours")

	// Branch theirs renames doc.txt -> doc_theirs.txt
	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "branch-rename-theirs")
	runGit(t, repo, "mv", "doc.txt", "doc_theirs.txt")
	runGit(t, repo, "commit", "-m", "rename to theirs")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "branch-rename-ours",
		TheirsRef: "branch-rename-theirs",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	if result.Summary.RenameConflicts != 1 {
		t.Fatalf("expected 1 rename conflict, got %d", result.Summary.RenameConflicts)
	}
	if len(result.Files) != 1 || result.Files[0].Status != compare.StatusRenameConflict {
		t.Fatalf("expected 1 file with StatusRenameConflict, got %+v", result.Files)
	}
}

// 10. Missing merge base within same repository (orphan branches)
func TestCompare_SameRepo_MissingMergeBase(t *testing.T) {
	repo := setupTestGitRepo(t)

	f1 := filepath.Join(repo, "file1.txt")
	_ = os.WriteFile(f1, []byte("root file\n"), 0644)
	runGit(t, repo, "add", "file1.txt")
	runGit(t, repo, "commit", "-m", "root commit")

	// Create orphan branch with disconnected history
	runGit(t, repo, "checkout", "--orphan", "orphan-branch")
	_ = os.Remove(f1)
	f2 := filepath.Join(repo, "file2.txt")
	_ = os.WriteFile(f2, []byte("orphan file\n"), 0644)
	runGit(t, repo, "add", "file2.txt")
	runGit(t, repo, "commit", "-m", "orphan commit")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "master",
		TheirsRef: "orphan-branch",
	})
	if err != nil {
		t.Fatalf("CompareCommits should succeed for orphan branches without panic: %v", err)
	}

	if result.MergeBaseResolved {
		t.Fatalf("expected MergeBaseResolved: false for orphan branches")
	}
	if !result.UnrelatedRepositories {
		t.Fatalf("expected UnrelatedRepositories: true for orphan branches")
	}
}

// 11. Multiple repositories with shared ancestry
func TestCompare_MultipleRepositories_SharedHistory(t *testing.T) {
	repo1 := setupTestGitRepo(t)

	sharedFile := filepath.Join(repo1, "shared.go")
	_ = os.WriteFile(sharedFile, []byte("package main\n\nfunc Common() int {\n    return 1\n}\n"), 0644)
	runGit(t, repo1, "add", "shared.go")
	runGit(t, repo1, "commit", "-m", "base common commit")

	// Clone repo1 into repo2
	repo2, err := os.MkdirTemp("", "compare_test_clone_*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo2) })

	runGit(t, repo2, "clone", repo1, ".")
	runGit(t, repo2, "config", "user.email", "test@commitissues.local")
	runGit(t, repo2, "config", "user.name", "Test User")

	// Commit on ours in repo1
	runGit(t, repo1, "checkout", "-b", "branch-ours")
	_ = os.WriteFile(sharedFile, []byte("package main\n\nfunc Common() int {\n    return 10\n}\n"), 0644)
	runGit(t, repo1, "commit", "-am", "modify in repo1")

	// Commit on theirs in repo2
	runGit(t, repo2, "checkout", "-b", "branch-theirs")
	repo2Shared := filepath.Join(repo2, "shared.go")
	_ = os.WriteFile(repo2Shared, []byte("package main\n\nfunc Common() int {\n    return 20\n}\n"), 0644)
	runGit(t, repo2, "commit", "-am", "modify in repo2")

	result, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo1,
		Repo2:     repo2,
		OursRef:   "branch-ours",
		TheirsRef: "branch-theirs",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed for cloned repos: %v", err)
	}

	if !result.MergeBaseResolved {
		t.Fatalf("expected MergeBaseResolved: true for cloned repos with shared base")
	}
	if result.UnrelatedRepositories {
		t.Fatalf("expected UnrelatedRepositories: false for cloned repos")
	}
	if result.Summary.StructuralCollisions != 1 {
		t.Fatalf("expected 1 structural collision, got %d", result.Summary.StructuralCollisions)
	}
}

// 12. No working-tree mutation during comparison
func TestCompare_ZeroWorkingTreeMutations(t *testing.T) {
	repo := setupTestGitRepo(t)

	f := filepath.Join(repo, "code.go")
	_ = os.WriteFile(f, []byte("package main\n\nfunc Calc() int { return 0 }\n"), 0644)
	runGit(t, repo, "add", "code.go")
	runGit(t, repo, "commit", "-m", "init")

	runGit(t, repo, "checkout", "-b", "b1")
	_ = os.WriteFile(f, []byte("package main\n\nfunc Calc() int { return 1 }\n"), 0644)
	runGit(t, repo, "commit", "-am", "b1")

	runGit(t, repo, "checkout", "master")
	runGit(t, repo, "checkout", "-b", "b2")
	_ = os.WriteFile(f, []byte("package main\n\nfunc Calc() int { return 2 }\n"), 0644)
	runGit(t, repo, "commit", "-am", "b2")

	// Leave working tree deliberately dirty: unstaged changes and untracked file
	dirtyFile := filepath.Join(repo, "dirty_untracked.txt")
	_ = os.WriteFile(dirtyFile, []byte("untracked file content"), 0644)
	_ = os.WriteFile(f, []byte("package main\n\nfunc Calc() int { return 999 }\n"), 0644)

	statusBefore := runGit(t, repo, "status", "--porcelain")

	// Run compare
	_, err := compare.CompareCommits(context.Background(), compare.Options{
		Repo1:     repo,
		OursRef:   "b1",
		TheirsRef: "b2",
	})
	if err != nil {
		t.Fatalf("CompareCommits failed: %v", err)
	}

	statusAfter := runGit(t, repo, "status", "--porcelain")

	if statusBefore != statusAfter {
		t.Fatalf("working tree was mutated!\nBefore:\n%s\nAfter:\n%s", statusBefore, statusAfter)
	}

	// Verify untracked file content intact
	data, err := os.ReadFile(dirtyFile)
	if err != nil || string(data) != "untracked file content" {
		t.Fatalf("dirty untracked file was modified or deleted")
	}
}
