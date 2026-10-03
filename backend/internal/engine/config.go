package engine

import (
	"fmt"
	"strconv"
	"time"

	ai "CommitIssues/internal/ai"
)

// ErrorCode is a stable, machine-readable identifier for a pipeline error.
type ErrorCode string

const (
	// CodeInvalidConfig indicates the supplied configuration failed validation.
	CodeInvalidConfig ErrorCode = "INVALID_CONFIG"
	// CodeCancelled indicates the run was cancelled via context cancellation.
	CodeCancelled ErrorCode = "CANCELLED"
	// CodeTimeout indicates the run exceeded its configured timeout.
	CodeTimeout ErrorCode = "TIMEOUT"
	// CodeGitStage indicates a staged conflict was missing a required stage.
	CodeGitStage ErrorCode = "GIT_MISSING_STAGE"
	// CodeFileParse indicates a file could not be parsed into an AST.
	CodeFileParse ErrorCode = "FILE_PARSE_ERROR"
	// CodeFileExtract indicates conflict versions could not be extracted.
	CodeFileExtract ErrorCode = "FILE_EXTRACT_ERROR"
	// CodeInternal indicates an unexpected internal failure for a file.
	CodeInternal ErrorCode = "INTERNAL_ERROR"
)

// CodedError is implemented by every structured engine error so callers can
// branch on a stable error code instead of matching message text.
type CodedError interface {
	error
	Code() ErrorCode
}

// ConfigError is a structured, deterministic configuration validation error.
// A single field is reported at a time in a fixed, documented order so that
// validation is deterministic.
type ConfigError struct {
	Field   string
	Value   string
	Rule    string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("invalid configuration: %s=%s violates rule %q: %s", e.Field, e.Value, e.Rule, e.Message)
}

// Code implements CodedError.
func (e *ConfigError) Code() ErrorCode { return CodeInvalidConfig }

// FileError is a structured per-file error. A single file failing never
// terminates the whole run; instead it is recorded against the file.
type FileError struct {
	File    string
	ErrCode ErrorCode
	Err     error
}

func (e *FileError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.File, e.ErrCode)
	}
	return fmt.Sprintf("%s: %s: %v", e.File, e.ErrCode, e.Err)
}

func (e *FileError) Unwrap() error { return e.Err }

// Code implements CodedError.
func (e *FileError) Code() ErrorCode { return e.ErrCode }

// Config holds the runtime options supplied by the user/CLI. AI provider
// settings live in the single shared ai.Config (flags > env > provider
// defaults); the pipeline-level fields below govern analysis only.
type Config struct {
	MaxConcurrency      int
	ConfidenceThreshold int
	Timeout             time.Duration

	// AI is the shared provider-configuration used only when runAI is true
	// (`resolve` and the Suggestions API). `analyze` and `serve` stay AI-free.
	AI ai.Config
}

// DefaultConfig returns a valid baseline configuration. Callers override the
// fields they care about and call Validate before scanning.
func DefaultConfig() Config {
	return Config{
		MaxConcurrency:      4,
		ConfidenceThreshold: 70,
		Timeout:             5 * time.Minute,
		AI:                  ai.Default(""),
	}
}

// Validate checks every configuration invariant in a fixed order. It must be
// called before repository scanning begins so that invalid settings never
// start analysis and can never produce a deadlock.
func (c Config) Validate() error {
	if c.MaxConcurrency < 1 {
		return &ConfigError{
			Field:   "MaxConcurrency",
			Value:   strconv.Itoa(c.MaxConcurrency),
			Rule:    "MaxConcurrency >= 1",
			Message: "max concurrency must be at least 1; a non-positive value would deadlock the processing semaphore",
		}
	}
	if c.Timeout <= 0 {
		return &ConfigError{
			Field:   "Timeout",
			Value:   c.Timeout.String(),
			Rule:    "Timeout > 0",
			Message: "timeout must be greater than zero",
		}
	}
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 100 {
		return &ConfigError{
			Field:   "ConfidenceThreshold",
			Value:   strconv.Itoa(c.ConfidenceThreshold),
			Rule:    "0 <= ConfidenceThreshold <= 100",
			Message: "confidence threshold must be a percentage between 0 and 100",
		}
	}
	return nil
}

// ErrorCodeOf returns the stable code for a structured error, defaulting to
// CodeInternal for unknown errors.
func ErrorCodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	if coded, ok := err.(CodedError); ok {
		return coded.Code()
	}
	return CodeInternal
}
