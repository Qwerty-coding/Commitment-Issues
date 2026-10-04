package engine

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/cache"
	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/validation"
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

// DefaultReadmeMaxBytes is the maximum byte size for a README excerpt.
// Truncation happens at a line boundary so the excerpt is always coherent.
const DefaultReadmeMaxBytes = 4096

// EnvTargetPromptTokens overrides the soft prompt-context budget (0 disables
// budgeting). The collision payload is never trimmed.
const EnvTargetPromptTokens = "AI_TARGET_PROMPT_TOKENS"

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

	// Validation holds user-configured post-apply validation settings.
	Validation validation.Config

	// ASTCache holds the optional incremental AST cache. If nil, engine uses a default in-memory cache.
	ASTCache cache.Cache

	// ReadmeContext enables including an excerpt of the repo-root README in the
	// prompt context. Enabled by default; disable via AI_README_CONTEXT=false
	// or --readme-context=false.
	ReadmeContext bool

	// ReadmeMaxBytes caps the README excerpt at this many bytes, truncated at a
	// line boundary. Governed by AI_README_MAX_BYTES / --readme-max-bytes.
	// The excerpt counts toward the AI_MAX_PROMPT_BYTES budget.
	ReadmeMaxBytes int

	// AITargetPromptTokens is the soft budget for the assembled prompt context
	// (README → functions → variables trim order). 0 disables budgeting. It
	// never trims the collision payload. Governed by AI_TARGET_PROMPT_TOKENS.
	AITargetPromptTokens int
}


// DefaultConfig returns a valid baseline configuration. Callers override the
// fields they care about and call Validate before scanning.
func DefaultConfig() Config {
	cfg := Config{
		MaxConcurrency:      4,
		ConfidenceThreshold: 70,
		Timeout:             5 * time.Minute,
		AI:                  ai.Default(""),
		Validation:          validation.Config{},
		ASTCache:             DefaultASTCache(),
		ReadmeContext:        true,
		ReadmeMaxBytes:       DefaultReadmeMaxBytes,
		AITargetPromptTokens: promptcontext.DefaultTargetPromptTokens,
	}
	// Apply environment overrides for the README feature.
	if v := os.Getenv(ai.EnvReadmeContext); v != "" {
		cfg.ReadmeContext = !equalsIgnoreCase(v, "false", "0", "no", "off")
	}
	if v := os.Getenv(ai.EnvReadmeMaxBytes); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ReadmeMaxBytes = n
		}
	}
	if v := os.Getenv(EnvTargetPromptTokens); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.AITargetPromptTokens = n
		}
	}
	return cfg
}

// DefaultASTCache builds the AST cache honouring the CACHE_* environment
// variables (CACHE_DISK_ENABLED, CACHE_DIR, CACHE_MAX_DISK_BYTES,
// CACHE_MAX_ENTRIES, CACHE_MAX_MEMORY_BYTES). If the environment specifies an
// unusable disk directory it falls back to a safe in-memory cache rather than
// failing the whole run.
func DefaultASTCache() cache.Cache {
	c, err := cache.NewFromEnv()
	if err != nil {
		return cache.NewDefault()
	}
	return c
}

// equalsIgnoreCase reports whether s equals any of the given targets
// (case-insensitive). Used for boolean-like env vars.
func equalsIgnoreCase(s string, targets ...string) bool {
	for _, t := range targets {
		if strings.EqualFold(s, t) {
			return true
		}
	}
	return false
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
	if c.AITargetPromptTokens < 0 {
		return &ConfigError{
			Field:   "AITargetPromptTokens",
			Value:   strconv.Itoa(c.AITargetPromptTokens),
			Rule:    "AITargetPromptTokens >= 0",
			Message: "target prompt tokens must be zero (disabled) or greater",
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
