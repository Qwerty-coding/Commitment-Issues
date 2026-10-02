package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ─── Error propagation ───────────────────────────────────────────────────────

func TestGetConflictedFiles_NonGitDirSurfacesError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clean.txt"), []byte("no markers here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := GetConflictedFiles(context.Background(), dir)
	if err == nil {
		t.Fatalf("a non-repository must surface a Git error, got files=%v", files)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a Git failure must not be reported as cancellation: %v", err)
	}
	var gitErr *GitError
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected *GitError, got %T: %v", err, err)
	}
}

func TestGetConflictedFiles_EmptyRepoIsNotAnError(t *testing.T) {
	dir := initRepo(t)
	files, err := GetConflictedFiles(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error for a valid empty repository: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no conflicts, got %v", files)
	}
}

// ─── Filesystem failure propagation ──────────────────────────────────────────

func TestGetConflictedFiles_BrokenSymlinkReadFailurePropagates(t *testing.T) {
	dir := initRepo(t)
	link := filepath.Join(dir, "dangling.txt")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist.txt"), link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	files, err := GetConflictedFiles(context.Background(), dir)
	if err == nil {
		t.Fatalf("a read failure must propagate, got files=%v", files)
	}
}

func TestGetConflictedFiles_UnreadableFilePropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}

	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "secret.txt"), "<<<<<<< ours\nx\n=======\ny\n>>>>>>> theirs\n")
	target := filepath.Join(dir, "secret.txt")
	if err := os.Chmod(target, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o644) })

	files, err := GetConflictedFiles(context.Background(), dir)
	if err == nil {
		t.Fatalf("an unreadable file must propagate an error, got files=%v", files)
	}
}

// ─── Cancellation ────────────────────────────────────────────────────────────

func TestGetConflictedFiles_Cancellation(t *testing.T) {
	dir := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files, err := GetConflictedFiles(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if files != nil {
		t.Fatalf("cancellation must never return a partial result: %v", files)
	}
}
