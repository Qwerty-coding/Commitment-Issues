package compare_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"CommitIssues/internal/compare"
)

func TestProbe_DebugCompare(t *testing.T) {
	dir, _ := os.MkdirTemp("", "probe_*")
	defer os.RemoveAll(dir)
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	run("init")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	p := filepath.Join(dir, "service.go")
	os.WriteFile(p, []byte("package main\n\nfunc ProcessData(x int) int {\n    return x\n}\n"), 0644)
	run("add", "service.go")
	run("commit", "-m", "base")
	run("checkout", "-b", "feature-ours")
	os.WriteFile(p, []byte("package main\n\nfunc ProcessData(x int) int {\n    return x + 10\n}\n"), 0644)
	run("commit", "-am", "ours")
	run("checkout", defaultBranch(t, dir))
	run("checkout", "-b", "feature-theirs")
	os.WriteFile(p, []byte("package main\n\nfunc ProcessData(x int) int {\n    return x * 2\n}\n"), 0644)
	run("commit", "-am", "theirs")

	brOut, brErr := run("rev-parse", "--abbrev-ref", "HEAD"); t.Logf("branch after checkouts: %q err=%v", brOut, brErr)
	stOut, stErr := run("status", "--porcelain"); t.Logf("status: %q err=%v", stOut, stErr)
	out, _ := run("gradle")
	_ = out

	res, err := compare.CompareCommits(context.Background(), compare.Options{Repo1: dir, OursRef: "feature-ours", TheirsRef: "feature-theirs"})
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	t.Logf("files: %+v", res.Files)
	t.Logf("summary: %+v", res.Summary)
}
