package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"CommitIssues/internal/runstate"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func writeConflictFile(t *testing.T, dir, name, ours, theirs string) {
	t.Helper()
	content := fmt.Sprintf("before\n<<<<<<< HEAD\n%s\n=======\n%s\n>>>>>>> feature\nafter\n", ours, theirs)
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validConfig() Config {
	return Config{MaxConcurrency: 2, Timeout: time.Minute, ConfidenceThreshold: 70}
}

// ─── Configuration validation ────────────────────────────────────────────────

func TestProcessRepository_InvalidConfigIsTerminal(t *testing.T) {
	cfg := Config{MaxConcurrency: 0, Timeout: time.Minute, ConfidenceThreshold: 70}
	// The repository does not exist: validation must reject before scanning.
	result, err := ProcessRepository(context.Background(), runstate.NewRun(), "/nonexistent/repo", []string{"a.txt"}, cfg, false)
	if err == nil {
		t.Fatal("expected configuration error")
	}
	if result != nil {
		t.Errorf("expected nil result on invalid config, got %+v", result)
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigError, got %T", err)
	}
}

// ─── Cancellation ────────────────────────────────────────────────────────────

func TestProcessRepository_CancelledBeforeStart(t *testing.T) {
	dir := t.TempDir()
	writeConflictFile(t, dir, "a.txt", "ours", "theirs")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ProcessRepository(ctx, runstate.NewRun(), dir, []string{"a.txt"}, validConfig(), false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestProcessRepository_DeadlineExceeded(t *testing.T) {
	dir := t.TempDir()
	writeConflictFile(t, dir, "a.txt", "ours", "theirs")

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	_, err := ProcessRepository(ctx, runstate.NewRun(), dir, []string{"a.txt"}, validConfig(), false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestProcessConflictFile_CancelledContext(t *testing.T) {
	dir := t.TempDir()
	writeConflictFile(t, dir, "a.txt", "ours", "theirs")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome := ProcessConflictFile(ctx, runstate.NewRun(), dir, "a.txt", validConfig(), false)
	if !errors.Is(outcome.Err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", outcome.Err)
	}
}

// ─── Per-file error isolation ────────────────────────────────────────────────

func TestProcessRepository_OneBadFileDoesNotFailRun(t *testing.T) {
	dir := t.TempDir()
	writeConflictFile(t, dir, "good.txt", "ours", "theirs")

	files := []string{"missing.txt", "good.txt"}
	result, err := ProcessRepository(context.Background(), runstate.NewRun(), dir, files, validConfig(), false)
	if err != nil {
		t.Fatalf("a single bad file must not be terminal, got %v", err)
	}
	if len(result.Outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(result.Outcomes))
	}
	if len(result.Succeeded) != 1 || len(result.Failed) != 1 {
		t.Fatalf("expected 1 success and 1 failure, got %d/%d", len(result.Succeeded), len(result.Failed))
	}
	var fileErr *FileError
	if !errors.As(result.Failed[0].Err, &fileErr) {
		t.Fatalf("expected *FileError, got %T", result.Failed[0].Err)
	}
	if fileErr.ErrCode != CodeFileExtract {
		t.Errorf("error code = %q, want %q", fileErr.ErrCode, CodeFileExtract)
	}
	if !strings.Contains(result.Summary(), "failed") {
		t.Errorf("summary should mention failures: %q", result.Summary())
	}
}

// ─── Determinism ─────────────────────────────────────────────────────────────

func deterministicRepoFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeConflictFile(t, dir, "c.txt", "c-ours", "c-theirs")
	writeConflictFile(t, dir, "a.txt", "a-ours", "a-theirs")
	writeConflictFile(t, dir, "b.txt", "b-ours", "b-theirs")
	return dir
}

func TestProcessRepository_OutputOrderIsDeterministic(t *testing.T) {
	dir := deterministicRepoFiles(t)
	files := []string{"c.txt", "a.txt", "b.txt"}

	runOnce := func(concurrency int) *RunResult {
		cfg := validConfig()
		cfg.MaxConcurrency = concurrency
		result, err := ProcessRepository(context.Background(), runstate.NewRun(), dir, files, cfg, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return result
	}

	serial := runOnce(1)
	parallel := runOnce(4)

	want := []string{"a.txt", "b.txt", "c.txt"}
	for i, name := range want {
		if serial.Outcomes[i].File != name {
			t.Errorf("serial outcome %d = %q, want %q", i, serial.Outcomes[i].File, name)
		}
		if parallel.Outcomes[i].File != name {
			t.Errorf("parallel outcome %d = %q, want %q", i, parallel.Outcomes[i].File, name)
		}
		if serial.Outcomes[i].Output != parallel.Outcomes[i].Output {
			t.Errorf("output for %s differs between concurrency levels", name)
		}
	}
}

func TestProcessRepository_RepeatedRunsProduceEquivalentResults(t *testing.T) {
	dir := deterministicRepoFiles(t)
	files := []string{"a.txt", "b.txt", "c.txt"}

	first, err := ProcessRepository(context.Background(), runstate.NewRun(), dir, files, validConfig(), false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProcessRepository(context.Background(), runstate.NewRun(), dir, files, validConfig(), false)
	if err != nil {
		t.Fatal(err)
	}

	if len(first.Outcomes) != len(second.Outcomes) {
		t.Fatalf("outcome count differs: %d vs %d", len(first.Outcomes), len(second.Outcomes))
	}
	for i := range first.Outcomes {
		if first.Outcomes[i].File != second.Outcomes[i].File {
			t.Errorf("file order differs at %d: %q vs %q", i, first.Outcomes[i].File, second.Outcomes[i].File)
		}
		if first.Outcomes[i].Output != second.Outcomes[i].Output {
			t.Errorf("output differs for %s between runs", first.Outcomes[i].File)
		}
	}
}

// ─── Concurrency safety ──────────────────────────────────────────────────────

func TestProcessRepository_MultipleWorkersRaceSafe(t *testing.T) {
	dir := t.TempDir()
	files := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("file%02d.txt", i)
		writeConflictFile(t, dir, name, fmt.Sprintf("ours-%d", i), fmt.Sprintf("theirs-%d", i))
		files = append(files, name)
	}

	cfg := validConfig()
	cfg.MaxConcurrency = 8
	result, err := ProcessRepository(context.Background(), runstate.NewRun(), dir, files, cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Outcomes) != len(files) {
		t.Fatalf("expected %d outcomes, got %d", len(files), len(result.Outcomes))
	}
}

func TestProcessRepository_NoGoroutineLeak(t *testing.T) {
	dir := deterministicRepoFiles(t)
	files := []string{"a.txt", "b.txt", "c.txt"}

	runtime.GC()
	before := runtime.NumGoroutine()

	cfg := validConfig()
	cfg.MaxConcurrency = 4
	if _, err := ProcessRepository(context.Background(), runstate.NewRun(), dir, files, cfg, false); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
}

// ─── Conflict region preservation through the pipeline ───────────────────────

func TestProcessConflictFile_PreservesMultipleRegions(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"<<<<<<< HEAD",
		"function alpha() { return 1; }",
		"=======",
		"function alpha() { return 2; }",
		">>>>>>> feature",
		"middle();",
		"<<<<<<< HEAD",
		"function beta() { return 1; }",
		"=======",
		"function beta() { return 2; }",
		">>>>>>> feature",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "multi.js"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	run := runstate.NewRun()
	outcome := ProcessConflictFile(context.Background(), run, dir, "multi.js", validConfig(), false)
	if outcome.Err != nil {
		t.Fatalf("unexpected error: %v", outcome.Err)
	}
	analysis, ok := run.GetAnalysis("multi.js")
	if !ok {
		t.Fatal("analysis should be registered")
	}
	if len(analysis.SmartDiff.Collisions) != 2 {
		t.Errorf("expected 2 collisions from 2 regions, got %d", len(analysis.SmartDiff.Collisions))
	}
}

// ─── Git stage error mapping ─────────────────────────────────────────────────

func runGitBuild(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestProcessConflictFile_MissingStageMapsToGitStageCode(t *testing.T) {
	dir := t.TempDir()
	runGitBuild(t, dir, "init", "-b", "main")
	runGitBuild(t, dir, "config", "user.email", "test@example.com")
	runGitBuild(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBuild(t, dir, "add", ".")
	runGitBuild(t, dir, "commit", "-m", "root")

	// add/add conflict: stage 1 (base) is missing.
	runGitBuild(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBuild(t, dir, "add", ".")
	runGitBuild(t, dir, "commit", "-m", "theirs")

	runGitBuild(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBuild(t, dir, "add", ".")
	runGitBuild(t, dir, "commit", "-m", "ours")

	merge := exec.Command("git", "merge", "--no-edit", "feature")
	merge.Dir = dir
	merge.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if err := merge.Run(); err == nil {
		t.Fatal("expected merge conflict")
	}

	outcome := ProcessConflictFile(context.Background(), runstate.NewRun(), dir, "file.txt", validConfig(), false)
	if outcome.Err == nil {
		t.Fatal("expected a missing-stage error")
	}
	var fileErr *FileError
	if !errors.As(outcome.Err, &fileErr) {
		t.Fatalf("expected *FileError, got %T: %v", outcome.Err, outcome.Err)
	}
	if fileErr.ErrCode != CodeGitStage {
		t.Errorf("error code = %q, want %q", fileErr.ErrCode, CodeGitStage)
	}
}
