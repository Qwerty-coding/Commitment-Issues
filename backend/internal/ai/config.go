package ai

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Configuration environment variables. Precedence is:
//  1. CLI flags / explicit struct overrides
//  2. Environment variables (AI_*)
//  3. Provider defaults
//
// Ollama is the default provider only when no provider is configured.
const (
	EnvProvider            = "AI_PROVIDER"
	EnvModel               = "AI_MODEL"
	EnvBaseURL             = "AI_BASE_URL"
	EnvAPIKey              = "AI_API_KEY"
	EnvTimeout             = "AI_TIMEOUT"
	EnvRetries             = "AI_RETRIES"
	EnvConfidenceThreshold = "AI_CONFIDENCE_THRESHOLD"
	EnvMaxPromptSize       = "AI_MAX_PROMPT_BYTES"
	EnvMaxResponseSize     = "AI_MAX_RESPONSE_BYTES"
	EnvRetryBackoff        = "AI_RETRY_BACKOFF"
	// EnvLegacyOllamaBaseURL keeps backward compatibility with the previously
	// documented OLLAMA_BASE_URL variable for the Ollama provider.
	EnvLegacyOllamaBaseURL = "OLLAMA_BASE_URL"
)

// Documented provider defaults. The default Ollama model is part of the
// project documentation and must remain explicit here.
const (
	// DefaultProvider is Ollama-first per the Phase 2 plan.
	DefaultProvider = "ollama"
	// DefaultOllamaModel is the documented default model for Ollama.
	DefaultOllamaModel = "qwen2:1.5b"
	// DefaultOllamaBaseURL is the standard local Ollama OpenAI-compatible port.
	DefaultOllamaBaseURL = "http://localhost:11434/v1"
	// DefaultGeminiModel is the documented default model for Gemini.
	DefaultGeminiModel = "gemini-3.5-flash"
	// DefaultGroqModel is the documented default model for Groq.
	DefaultGroqModel = "llama-3.3-70b-versatile"
	// DefaultGroqBaseURL is Groq's OpenAI-compatible endpoint.
	DefaultGroqBaseURL = "https://api.groq.com/openai/v1"
	// geminiAPIBaseURL is the Gemini Generative Language API root.
	geminiAPIBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	// DefaultRequestTimeout bounds a single AI request.
	DefaultRequestTimeout = 60 * time.Second
	// DefaultRetryCount is the number of retries after the first attempt.
	DefaultRetryCount = 2
	// DefaultConfidenceThreshold flags suggestions below this confidence.
	DefaultConfidenceThreshold = 70
	// DefaultMaxPromptSize bounds the prompt payload sent to a provider.
	DefaultMaxPromptSize = 512 * 1024
	// DefaultMaxResponseSize bounds the accepted HTTP response body.
	DefaultMaxResponseSize = 1024 * 1024
	// DefaultRetryBackoff is the initial retry backoff; it grows exponentially
	// and is capped by MaxRetryBackoff.
	DefaultRetryBackoff = 500 * time.Millisecond
	// MaxRetryBackoff caps exponential growth.
	MaxRetryBackoff = 4 * time.Second
)

// Config is the single shared AI configuration used by the CLI, the backend
// API and suggestion generation. It replaces the previous ad-hoc per-callsite
// provider/model literals.
type Config struct {
	Provider            string        `json:"provider"`
	Model               string        `json:"model"`
	BaseURL             string        `json:"baseUrl,omitempty"`
	APIKey              string        `json:"-"`
	RequestTimeout      time.Duration `json:"requestTimeout"`
	RetryCount          int           `json:"retryCount"`
	RetryBackoff        time.Duration `json:"retryBackoff,omitempty"`
	ConfidenceThreshold int           `json:"confidenceThreshold"`
	MaxPromptSize       int           `json:"maxPromptSize"`
	MaxResponseSize     int           `json:"maxResponseSize"`
}

// AIConfig is retained as an alias so existing call sites keep compiling
// while the richer unified Config replaces the four-field original.
type AIConfig = Config

// Default returns the documented defaults for the given provider. An unknown
// or empty provider falls back to Ollama (the documented Ollama-first default).
func Default(provider string) Config {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini":
		return Config{
			Provider:            "gemini",
			Model:               DefaultGeminiModel,
			RequestTimeout:      DefaultRequestTimeout,
			RetryCount:          DefaultRetryCount,
			RetryBackoff:        DefaultRetryBackoff,
			ConfidenceThreshold: DefaultConfidenceThreshold,
			MaxPromptSize:       DefaultMaxPromptSize,
			MaxResponseSize:     DefaultMaxResponseSize,
		}
	case "groq":
		return Config{
			Provider:            "groq",
			Model:               DefaultGroqModel,
			BaseURL:             DefaultGroqBaseURL,
			RequestTimeout:      DefaultRequestTimeout,
			RetryCount:          DefaultRetryCount,
			RetryBackoff:        DefaultRetryBackoff,
			ConfidenceThreshold: DefaultConfidenceThreshold,
			MaxPromptSize:       DefaultMaxPromptSize,
			MaxResponseSize:     DefaultMaxResponseSize,
		}
	default:
		return Config{
			Provider:            DefaultProvider,
			Model:               DefaultOllamaModel,
			BaseURL:             DefaultOllamaBaseURL,
			RequestTimeout:      DefaultRequestTimeout,
			RetryCount:          DefaultRetryCount,
			RetryBackoff:        DefaultRetryBackoff,
			ConfidenceThreshold: DefaultConfidenceThreshold,
			MaxPromptSize:       DefaultMaxPromptSize,
			MaxResponseSize:     DefaultMaxResponseSize,
		}
	}
}

// ConfigFromEnv builds the configuration from environment variables over
// provider defaults. Explicit overrides (CLI flags, struct fields) are applied
// afterwards by the caller, preserving the documented precedence:
// flags > environment > defaults.
//
// Never silently switches providers or models: an explicitly configured value
// is used as-is or validation fails.
func ConfigFromEnv() Config {
	cfg := Default(os.Getenv(EnvProvider))
	if v := strings.TrimSpace(os.Getenv(EnvProvider)); v != "" {
		cfg.Provider = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv(EnvModel)); v != "" {
		cfg.Model = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvBaseURL)); v != "" {
		cfg.BaseURL = v
	} else if strings.EqualFold(cfg.Provider, "ollama") {
		// Legacy variable kept for backward compatibility.
		if v := strings.TrimSpace(os.Getenv(EnvLegacyOllamaBaseURL)); v != "" {
			cfg.BaseURL = v
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvAPIKey)); v != "" {
		cfg.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvTimeout)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.RequestTimeout = d
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvRetries)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.RetryCount = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvConfidenceThreshold)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100 {
			cfg.ConfidenceThreshold = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvMaxPromptSize)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxPromptSize = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvMaxResponseSize)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxResponseSize = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvRetryBackoff)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.RetryBackoff = d
		}
	}
	return cfg
}

// ConfigError is a structured, deterministic AI configuration error.
type ConfigError struct {
	Field   string
	Value   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("invalid AI configuration: %s=%s: %s", e.Field, e.Value, e.Message)
}

// Code implements the coded error contract.
func (e *ConfigError) Code() ErrorCode { return CodeConfigInvalid }

// Validate checks the configuration in a fixed, deterministic order. The
// API key value is never included in error output.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Provider) == "" {
		return &ConfigError{Field: "Provider", Value: "", Message: "provider is required"}
	}
	registered := false
	for _, name := range ListProviders() {
		if strings.EqualFold(name, c.Provider) {
			registered = true
			break
		}
	}
	if !registered {
		return &ConfigError{
			Field:   "Provider",
			Value:   c.Provider,
			Message: fmt.Sprintf("unsupported provider; available: %s", strings.Join(ListProviders(), ", ")),
		}
	}
	if strings.TrimSpace(c.Model) == "" {
		return &ConfigError{Field: "Model", Value: "", Message: "model is required"}
	}
	if c.RequestTimeout <= 0 {
		return &ConfigError{
			Field:   "RequestTimeout",
			Value:   c.RequestTimeout.String(),
			Message: "request timeout must be greater than zero",
		}
	}
	if c.RetryCount < 0 {
		return &ConfigError{
			Field:   "RetryCount",
			Value:   strconv.Itoa(c.RetryCount),
			Message: "retry count must be zero or greater",
		}
	}
	if c.RetryBackoff < 0 {
		return &ConfigError{
			Field:   "RetryBackoff",
			Value:   c.RetryBackoff.String(),
			Message: "retry backoff must be zero or greater",
		}
	}
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 100 {
		return &ConfigError{
			Field:   "ConfidenceThreshold",
			Value:   strconv.Itoa(c.ConfidenceThreshold),
			Message: "confidence threshold must be between 0 and 100",
		}
	}
	if c.MaxPromptSize <= 0 {
		return &ConfigError{
			Field:   "MaxPromptSize",
			Value:   strconv.Itoa(c.MaxPromptSize),
			Message: "maximum prompt size must be greater than zero",
		}
	}
	if c.MaxResponseSize <= 0 {
		return &ConfigError{
			Field:   "MaxResponseSize",
			Value:   strconv.Itoa(c.MaxResponseSize),
			Message: "maximum response size must be greater than zero",
		}
	}
	if _, err := ValidateBaseURL(c.BaseURL, c.Provider); err != nil {
		return err
	}
	return nil
}

var localhostURL = regexp.MustCompile(`^https?://`)

// ValidateBaseURL parses the configured base URL. An empty value is
// acceptable: each provider falls back to its documented default endpoint at
// construction. A non-empty value must be a parseable http(s) URL.
func ValidateBaseURL(raw, provider string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil // provider default applied at construction
	}
	if !localhostURL.MatchString(trimmed) {
		return nil, &ConfigError{Field: "BaseURL", Value: trimmed, Message: "base URL must start with http:// or https://"}
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return nil, &ConfigError{Field: "BaseURL", Value: trimmed, Message: "base URL could not be parsed"}
	}
	return parsed, nil
}

// Sanitized returns a copy of the configuration safe for logs and API
// metadata: the API key is removed entirely.
func (c Config) Sanitized() Config {
	out := c
	out.APIKey = ""
	return out
}
