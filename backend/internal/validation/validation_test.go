package validation

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestValidation_DisallowedCommandRejected(t *testing.T) {
	cfg := Config{
		RepoRoot:  t.TempDir(),
		AllowList: []string{"go", "npm"},
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	result := runner.Run(context.Background(), "rm", []string{"-rf", "/"})
	if result.Status != StatusFailed {
		t.Errorf("expected StatusFailed, got %s", result.Status)
	}
	if !strings.Contains(result.ErrorMessage, "not in the allow-list") {
		t.Errorf("expected allow-list error message, got: %s", result.ErrorMessage)
	}
}

func TestValidation_EmptyAllowListRejected(t *testing.T) {
	cfg := Config{
		RepoRoot:  t.TempDir(),
		AllowList: []string{},
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	result := runner.Run(context.Background(), "go", []string{"test"})
	if result.Status != StatusFailed {
		t.Errorf("expected StatusFailed, got %s", result.Status)
	}
}

func TestValidation_Success(t *testing.T) {
	cfg := Config{
		RepoRoot:  t.TempDir(),
		AllowList: []string{"go"},
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	result := runner.Run(context.Background(), "go", []string{"version"})
	if result.Status != StatusPassed {
		t.Errorf("expected StatusPassed, got %s (err: %s)", result.Status, result.ErrorMessage)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "go version") {
		t.Errorf("expected go version output, got: %s", result.Output)
	}
}

func TestValidation_FailureExitCode(t *testing.T) {
	cfg := Config{
		RepoRoot:  t.TempDir(),
		AllowList: []string{"go"},
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// 'go non_existent_subcommand' will exit with non-zero
	result := runner.Run(context.Background(), "go", []string{"nonexistentcommandthatfails"})
	if result.Status != StatusFailed {
		t.Errorf("expected StatusFailed, got %s", result.Status)
	}
	if result.ExitCode == 0 {
		t.Errorf("expected non-zero exit code, got %d", result.ExitCode)
	}
}

func TestValidation_Cancellation(t *testing.T) {
	cfg := Config{
		RepoRoot:  t.TempDir(),
		AllowList: []string{"go"},
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before run

	result := runner.Run(ctx, "go", []string{"version"})
	if result.Status != StatusCancelled {
		t.Errorf("expected StatusCancelled, got %s", result.Status)
	}
}

func TestValidation_Timeout(t *testing.T) {
	cfg := Config{
		RepoRoot:  t.TempDir(),
		AllowList: []string{"go"},
		Timeout:   1 * time.Nanosecond, // immediate timeout
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Millisecond) // ensure timeout elapsed
	result := runner.Run(context.Background(), "go", []string{"version"})
	if result.Status != StatusTimedOut && result.Status != StatusCancelled {
		t.Errorf("expected StatusTimedOut or StatusCancelled, got %s", result.Status)
	}
}

func TestValidation_OutputSizeLimit(t *testing.T) {
	cfg := Config{
		RepoRoot:    t.TempDir(),
		AllowList:   []string{"go"},
		OutputLimit: 20, // very small limit to test truncation
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	result := runner.Run(context.Background(), "go", []string{"version"})
	if len(result.Output) > 100 {
		t.Errorf("expected truncated output, got len %d: %s", len(result.Output), result.Output)
	}
	if !strings.Contains(result.Output, "truncated") {
		t.Errorf("expected truncation notice in output: %s", result.Output)
	}
}
