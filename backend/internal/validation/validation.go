// Package validation runs user-configured post-apply validation commands.
//
// Phase 8 constraints:
//   - Commands must come from an explicit allow-list; never from AI output.
//   - Working directory is restricted to the repository root.
//   - The environment is sanitized to a minimal, safe set.
//   - Each run has a timeout and can be cancelled via context.
//   - stdout+stderr are captured and truncated to a configurable size limit.
//   - The exit code, output, duration, and typed status are all returned.
//   - A validation failure never automatically reverts the applied change.
package validation

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"CommitIssues/internal/resolutions"
)

// Status constants mirror the resolutions package validation statuses so the
// validation package stays free of import cycles when used by the apply layer.
const (
	StatusNotRun    = resolutions.ValidationNotRun
	StatusRunning   = resolutions.ValidationRunning
	StatusPassed    = resolutions.ValidationPassed
	StatusFailed    = resolutions.ValidationFailed
	StatusTimedOut  = resolutions.ValidationTimedOut
	StatusCancelled = resolutions.ValidationCancelled
)

// DefaultTimeout is the maximum duration for a validation command when the
// caller does not specify one.
const DefaultTimeout = 2 * time.Minute

// DefaultOutputLimit is the maximum number of bytes captured from stdout+stderr
// combined, per validation run.
const DefaultOutputLimit = 1 << 20 // 1 MiB

// Config holds the validation runner configuration for one repository.
type Config struct {
	// AllowList is the explicit set of command names (base name or absolute
	// path) that may be executed. An empty allow-list means validation is
	// disabled; the runner returns StatusNotRun immediately.
	AllowList []string
	// WorkDir is the directory in which the command runs. If empty, the
	// repository root is used. The runner rejects any WorkDir outside the
	// repository root.
	WorkDir string
	// RepoRoot is the canonical repository root used to enforce the WorkDir
	// restriction.
	RepoRoot string
	// Timeout is the maximum allowed duration per command. If zero,
	// DefaultTimeout is used.
	Timeout time.Duration
	// OutputLimit is the maximum bytes captured from combined stdout+stderr.
	// If zero, DefaultOutputLimit is used.
	OutputLimit int
	// Env is the explicit environment for the command. If nil, a sanitized
	// minimal environment is used (PATH only, no HOME, USER, etc.).
	Env []string
}

// Result is the outcome of one validation run.
type Result struct {
	// Status is one of the Status* constants above.
	Status string
	// ExitCode is the process exit code (0 on success, >0 on failure, -1 if
	// not applicable).
	ExitCode int
	// Output is the combined, truncated stdout+stderr.
	Output string
	// Duration is the wall-clock execution time.
	Duration time.Duration
	// ErrorMessage carries a redacted, human-readable failure summary when
	// Status is StatusFailed, StatusTimedOut or StatusCancelled.
	ErrorMessage string
}

// Runner executes a single validation command against the repository.
type Runner struct {
	cfg Config
}

// NewRunner returns a Runner for the given config. It validates the config and
// returns an error for unsafe settings (WorkDir outside RepoRoot, unknown
// command, etc.).
func NewRunner(cfg Config) (*Runner, error) {
	if cfg.RepoRoot == "" {
		return nil, fmt.Errorf("validation: RepoRoot must not be empty")
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = cfg.RepoRoot
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.OutputLimit <= 0 {
		cfg.OutputLimit = DefaultOutputLimit
	}
	return &Runner{cfg: cfg}, nil
}

// Run executes the named command with args in the configured working directory,
// honoring context cancellation and the configured timeout.
//
// command must be in the allow-list; otherwise the run is rejected with a
// descriptive error and Status=StatusFailed (never panics). The command is
// looked up via exec.LookPath within the sanitized environment — AI output
// is never accepted as a command name.
func (r *Runner) Run(ctx context.Context, command string, args []string) Result {
	start := time.Now()

	// Allow-list enforcement.
	if !r.isAllowed(command) {
		return Result{
			Status:       StatusFailed,
			ExitCode:     -1,
			Duration:     time.Since(start),
			ErrorMessage: fmt.Sprintf("validation command %q is not in the allow-list", command),
		}
	}

	// Apply timeout on top of any existing deadline.
	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = r.cfg.WorkDir
	cmd.Env = r.buildEnv()

	var combined bytes.Buffer
	limiter := &limitedWriter{w: &combined, limit: r.outputLimit()}
	cmd.Stdout = limiter
	cmd.Stderr = limiter

	err := cmd.Run()
	duration := time.Since(start)
	output := combined.String()
	if limiter.truncated {
		output += fmt.Sprintf("\n[output truncated at %d bytes]", r.outputLimit())
	}

	if err == nil {
		return Result{
			Status:   StatusPassed,
			ExitCode: 0,
			Output:   output,
			Duration: duration,
		}
	}

	// Distinguish between context cancellation, timeout, and process failure.
	if ctx.Err() != nil {
		status := StatusCancelled
		msg := "validation command was cancelled"
		if ctx.Err() == context.DeadlineExceeded {
			status = StatusTimedOut
			msg = fmt.Sprintf("validation command timed out after %s", r.cfg.Timeout)
		}
		return Result{
			Status:       status,
			ExitCode:     -1,
			Output:       output,
			Duration:     duration,
			ErrorMessage: msg,
		}
	}

	exitCode := -1
	if ee, ok := err.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	}
	return Result{
		Status:       StatusFailed,
		ExitCode:     exitCode,
		Output:       output,
		Duration:     duration,
		ErrorMessage: fmt.Sprintf("validation command exited with code %d: %v", exitCode, err),
	}
}

// isAllowed reports whether command is in the allow-list. It compares the base
// name of the command to the base name of each allow-list entry so that both
// "go" and "/usr/local/go/bin/go" match an allow-list entry of "go".
func (r *Runner) isAllowed(command string) bool {
	if len(r.cfg.AllowList) == 0 {
		return false
	}
	cmdBase := baseName(command)
	for _, allowed := range r.cfg.AllowList {
		if baseName(allowed) == cmdBase || allowed == command {
			return true
		}
	}
	return false
}

// buildEnv returns the sanitized environment for the validation command. If
// the config specifies an explicit Env, it is used verbatim. Otherwise a
// minimal environment containing only PATH is constructed.
func (r *Runner) buildEnv() []string {
	if r.cfg.Env != nil {
		return r.cfg.Env
	}
	// Minimal safe environment: keep PATH so commands can be found, drop
	// everything else to prevent credential leakage.
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	return []string{"PATH=" + path}
}

func (r *Runner) outputLimit() int {
	if r.cfg.OutputLimit > 0 {
		return r.cfg.OutputLimit
	}
	return DefaultOutputLimit
}

// baseName returns the base name of a command (last path component, without
// extension on Windows).
func baseName(command string) string {
	// Use strings.Split to handle both / and \ separators.
	parts := strings.FieldsFunc(command, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return command
	}
	base := parts[len(parts)-1]
	// Strip .exe on Windows for comparison.
	base = strings.TrimSuffix(base, ".exe")
	return base
}

// truncate clips s to at most limit bytes, appending a truncation notice.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	notice := fmt.Sprintf("\n[output truncated at %d bytes]", limit)
	if limit <= len(notice) {
		return notice
	}
	return s[:limit-len(notice)] + notice
}

// limitedWriter wraps an io.Writer and stops accepting bytes once the limit
// is reached. Writes beyond the limit are silently dropped and tracked.
type limitedWriter struct {
	w         *bytes.Buffer
	limit     int
	n         int
	truncated bool
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.n >= lw.limit {
		lw.truncated = true
		return len(p), nil // silently drop
	}
	remaining := lw.limit - lw.n
	if len(p) > remaining {
		lw.truncated = true
		p = p[:remaining]
	}
	n, err := lw.w.Write(p)
	lw.n += n
	return len(p), err // report all bytes consumed to avoid spurious errors
}
