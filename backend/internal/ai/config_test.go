package ai

import (
	"strings"
	"testing"
	"time"
)

func TestDefault_OllamaFirst(t *testing.T) {
	for _, provider := range []string{"", "ollama", "OLLAMA", "unknown"} {
		cfg := Default(provider)
		if cfg.Provider != "ollama" {
			t.Errorf("Default(%q).Provider = %q, want ollama", provider, cfg.Provider)
		}
		if cfg.Model != DefaultOllamaModel {
			t.Errorf("Default(%q).Model = %q, want %q", provider, cfg.Model, DefaultOllamaModel)
		}
		if cfg.BaseURL != DefaultOllamaBaseURL {
			t.Errorf("Default(%q).BaseURL = %q, want %q", provider, cfg.BaseURL, DefaultOllamaBaseURL)
		}
	}
}

func TestDefault_GeminiAndGroq(t *testing.T) {
	gem := Default("gemini")
	if gem.Provider != "gemini" || gem.Model != DefaultGeminiModel {
		t.Errorf("gemini defaults = %+v", gem)
	}
	// Gemini uses a fixed documented endpoint; an empty BaseURL is valid and
	// the resolver applies geminiAPIBaseURL.
	if err := gem.Validate(); err != nil {
		t.Errorf("gemini defaults must validate: %v", err)
	}
	groq := Default("groq")
	if groq.Model != DefaultGroqModel || groq.BaseURL != DefaultGroqBaseURL {
		t.Errorf("groq defaults = %+v", groq)
	}
}

func TestDefault_HasBoundedLimits(t *testing.T) {
	cfg := Default("")
	if cfg.RequestTimeout != DefaultRequestTimeout || cfg.RetryCount != DefaultRetryCount {
		t.Errorf("timeouts/retries not defaulted: %+v", cfg)
	}
	if cfg.ConfidenceThreshold != DefaultConfidenceThreshold {
		t.Errorf("confidence threshold = %d", cfg.ConfidenceThreshold)
	}
	if cfg.MaxPromptSize <= 0 || cfg.MaxResponseSize <= 0 {
		t.Errorf("size limits must be positive: %+v", cfg)
	}
}

func TestConfigFromEnv_EnvOverridesDefaults(t *testing.T) {
	t.Setenv("AI_PROVIDER", "groq")
	t.Setenv("AI_MODEL", "custom-model")
	t.Setenv("AI_BASE_URL", "http://example.test/v1")
	t.Setenv("AI_API_KEY", "secret-key")
	t.Setenv("AI_TIMEOUT", "12s")
	t.Setenv("AI_RETRIES", "5")
	t.Setenv("AI_CONFIDENCE_THRESHOLD", "42")
	t.Setenv("AI_MAX_PROMPT_BYTES", "1234")
	t.Setenv("AI_MAX_RESPONSE_BYTES", "5678")
	t.Setenv("AI_RETRY_BACKOFF", "7ms")

	cfg := ConfigFromEnv()
	if cfg.Provider != "groq" || cfg.Model != "custom-model" || cfg.BaseURL != "http://example.test/v1" {
		t.Errorf("provider/model/base not from env: %+v", cfg)
	}
	if cfg.APIKey != "secret-key" {
		t.Errorf("api key not from env")
	}
	if cfg.RequestTimeout != 12*time.Second || cfg.RetryCount != 5 || cfg.ConfidenceThreshold != 42 {
		t.Errorf("numeric env not applied: %+v", cfg)
	}
	if cfg.MaxPromptSize != 1234 || cfg.MaxResponseSize != 5678 {
		t.Errorf("size env not applied: %+v", cfg)
	}
	if cfg.RetryBackoff != 7*time.Millisecond {
		t.Errorf("backoff env not applied: %v", cfg.RetryBackoff)
	}
}

func TestConfigFromEnv_DefaultsWhenUnset(t *testing.T) {
	// Clear all AI_* variables for a deterministic default path.
	for _, k := range []string{"AI_PROVIDER", "AI_MODEL", "AI_BASE_URL", "AI_API_KEY", "AI_TIMEOUT",
		"AI_RETRIES", "AI_CONFIDENCE_THRESHOLD", "AI_MAX_PROMPT_BYTES", "AI_MAX_RESPONSE_BYTES",
		"AI_RETRY_BACKOFF", "OLLAMA_BASE_URL"} {
		t.Setenv(k, "")
	}
	cfg := ConfigFromEnv()
	if cfg.Provider != "ollama" || cfg.Model != DefaultOllamaModel {
		t.Errorf("expected Ollama-first defaults, got %+v", cfg)
	}
}

func TestConfigFromEnv_LegacyOllamaBaseURL(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://legacy.test:11434/v1")
	for _, k := range []string{"AI_PROVIDER", "AI_BASE_URL"} {
		t.Setenv(k, "")
	}
	cfg := ConfigFromEnv()
	if cfg.BaseURL != "http://legacy.test:11434/v1" {
		t.Errorf("legacy OLLAMA_BASE_URL not honored, got %q", cfg.BaseURL)
	}
	// AI_BASE_URL must win over the legacy variable.
	t.Setenv("AI_BASE_URL", "http://new.test/v1")
	if cfg := ConfigFromEnv(); cfg.BaseURL != "http://new.test/v1" {
		t.Errorf("AI_BASE_URL should override legacy, got %q", cfg.BaseURL)
	}
}

func TestConfigFromEnv_InvalidValuesIgnored(t *testing.T) {
	t.Setenv("AI_TIMEOUT", "not-a-duration")
	t.Setenv("AI_RETRIES", "-3")
	t.Setenv("AI_CONFIDENCE_THRESHOLD", "999")
	t.Setenv("AI_MAX_PROMPT_BYTES", "-1")
	cfg := ConfigFromEnv()
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("invalid timeout should keep the default, got %v", cfg.RequestTimeout)
	}
	if cfg.RetryCount != DefaultRetryCount {
		t.Errorf("invalid retries should keep the default, got %d", cfg.RetryCount)
	}
	if cfg.ConfidenceThreshold != DefaultConfidenceThreshold {
		t.Errorf("invalid threshold should keep the default, got %d", cfg.ConfidenceThreshold)
	}
	if cfg.MaxPromptSize != DefaultMaxPromptSize {
		t.Errorf("invalid size should keep the default, got %d", cfg.MaxPromptSize)
	}
}

func TestApplyProviderDefaults_FlagSelectedProvider(t *testing.T) {
	// Simulate the CLI scenario: no AI_* variables set, so
	// ConfigFromEnv bakes in the Ollama fallback defaults, then
	// a flag selects a different provider.
	for _, k := range []string{"AI_PROVIDER", "AI_MODEL", "AI_BASE_URL", "OLLAMA_BASE_URL"} {
		t.Setenv(k, "")
	}

	cfg := ConfigFromEnv()
	cfg.Provider = "gemini"
	if cfg.Model != DefaultOllamaModel {
		t.Fatalf("precondition failed: model is %q, want the Ollama fallback default", cfg.Model)
	}

	fixed := cfg.ApplyProviderDefaults(cfg.Provider)
	if fixed.Model != DefaultGeminiModel {
		t.Errorf("ApplyProviderDefaults(gemini).Model = %q, want %q", fixed.Model, DefaultGeminiModel)
	}
	// Gemini's documented default endpoint is applied by the
	// resolver, so the default base URL is empty.
	if fixed.BaseURL != "" {
		t.Errorf("ApplyProviderDefaults(gemini).BaseURL = %q, want empty (resolver default)", fixed.BaseURL)
	}
	if err := fixed.Validate(); err != nil {
		t.Errorf("fixed config must validate: %v", err)
	}
}

func TestApplyProviderDefaults_EveryProviderWorksAlone(t *testing.T) {
	for _, k := range []string{"AI_PROVIDER", "AI_MODEL", "AI_BASE_URL", "OLLAMA_BASE_URL"} {
		t.Setenv(k, "")
	}
	cases := []struct {
		provider string
		model    string
		baseURL  string
	}{
		{"gemini", DefaultGeminiModel, ""},
		{"groq", DefaultGroqModel, DefaultGroqBaseURL},
		{"ollama", DefaultOllamaModel, DefaultOllamaBaseURL},
		{"GEMINI", DefaultGeminiModel, ""}, // case-insensitive
	}
	for _, tc := range cases {
		cfg := ConfigFromEnv()
		cfg.Provider = tc.provider
		fixed := cfg.ApplyProviderDefaults(tc.provider)
		if fixed.Model != tc.model {
			t.Errorf("ApplyProviderDefaults(%q).Model = %q, want %q", tc.provider, fixed.Model, tc.model)
		}
		if fixed.BaseURL != tc.baseURL {
			t.Errorf("ApplyProviderDefaults(%q).BaseURL = %q, want %q", tc.provider, fixed.BaseURL, tc.baseURL)
		}
	}
}

func TestApplyProviderDefaults_PreservesEnvModel(t *testing.T) {
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("AI_MODEL", "env-model")
	t.Setenv("AI_BASE_URL", "")
	t.Setenv("OLLAMA_BASE_URL", "")

	cfg := ConfigFromEnv()
	cfg.Provider = "groq"
	fixed := cfg.ApplyProviderDefaults(cfg.Provider)
	if fixed.Model != "env-model" {
		t.Errorf("env model must win over provider defaults, got %q", fixed.Model)
	}
	if fixed.BaseURL != DefaultGroqBaseURL {
		t.Errorf("groq base URL default = %q, want %q", fixed.BaseURL, DefaultGroqBaseURL)
	}
}

func TestApplyProviderDefaults_PreservesEnvBaseURL(t *testing.T) {
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_BASE_URL", "http://relay.example/v1")
	t.Setenv("OLLAMA_BASE_URL", "")

	cfg := ConfigFromEnv()
	cfg.Provider = "gemini"
	fixed := cfg.ApplyProviderDefaults(cfg.Provider)
	if fixed.BaseURL != "http://relay.example/v1" {
		t.Errorf("env base URL must win over provider defaults, got %q", fixed.BaseURL)
	}
	if fixed.Model != DefaultGeminiModel {
		t.Errorf("model default = %q, want %q", fixed.Model, DefaultGeminiModel)
	}
}

func TestApplyProviderDefaults_LegacyOllamaBaseURLKept(t *testing.T) {
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_BASE_URL", "")
	t.Setenv("OLLAMA_BASE_URL", "http://legacy.test:11434/v1")

	cfg := ConfigFromEnv()
	cfg.Provider = "ollama"
	fixed := cfg.ApplyProviderDefaults(cfg.Provider)
	if fixed.BaseURL != "http://legacy.test:11434/v1" {
		t.Errorf("legacy OLLAMA_BASE_URL must be preserved, got %q", fixed.BaseURL)
	}
	if fixed.Model != DefaultOllamaModel {
		t.Errorf("model default = %q, want %q", fixed.Model, DefaultOllamaModel)
	}
}

func TestApplyProviderDefaults_StaleFallbackURLReplaced(t *testing.T) {
	// A legacy Ollama URL must not leak into a provider selected
	// via a flag.
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_BASE_URL", "")
	t.Setenv("OLLAMA_BASE_URL", "http://legacy.test:11434/v1")

	cfg := ConfigFromEnv()
	if cfg.BaseURL != "http://legacy.test:11434/v1" {
		t.Fatalf("precondition failed: legacy URL not applied, got %q", cfg.BaseURL)
	}
	cfg.Provider = "gemini"
	fixed := cfg.ApplyProviderDefaults(cfg.Provider)
	if fixed.BaseURL != "" {
		t.Errorf("stale Ollama URL leaked into gemini config: %q", fixed.BaseURL)
	}
}

func TestValidate_DeterministicOrder(t *testing.T) {
	base := Default("")

	cases := []struct {
		name  string
		mut   func(*Config)
		field string
	}{
		{"provider empty", func(c *Config) { c.Provider = "" }, "Provider"},
		{"provider unsupported", func(c *Config) { c.Provider = "nope" }, "Provider"},
		{"model empty", func(c *Config) { c.Model = "" }, "Model"},
		{"timeout zero", func(c *Config) { c.RequestTimeout = 0 }, "RequestTimeout"},
		{"retries negative", func(c *Config) { c.RetryCount = -1 }, "RetryCount"},
		{"backoff negative", func(c *Config) { c.RetryBackoff = -time.Second }, "RetryBackoff"},
		{"threshold too high", func(c *Config) { c.ConfidenceThreshold = 101 }, "ConfidenceThreshold"},
		{"threshold negative", func(c *Config) { c.ConfidenceThreshold = -1 }, "ConfidenceThreshold"},
		{"prompt size zero", func(c *Config) { c.MaxPromptSize = 0 }, "MaxPromptSize"},
		{"response size zero", func(c *Config) { c.MaxResponseSize = 0 }, "MaxResponseSize"},
		{"base url missing scheme", func(c *Config) { c.BaseURL = "localhost:11434" }, "BaseURL"},
		{"base url unparsable", func(c *Config) { c.BaseURL = "http://" }, "BaseURL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mut(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
			var ce *ConfigError
			if !asConfigError(err, &ce) {
				t.Fatalf("expected *ConfigError, got %T", err)
			}
			if ce.Field != tc.field {
				t.Errorf("first failing field = %q, want %q", ce.Field, tc.field)
			}
			if ce.Code() != CodeConfigInvalid {
				t.Errorf("config error code = %q", ce.Code())
			}
		})
	}
}

func asConfigError(err error, target **ConfigError) bool {
	ce, ok := err.(*ConfigError)
	if ok {
		*target = ce
	}
	return ok
}

func TestValidate_AcceptsValidConfig(t *testing.T) {
	for _, provider := range []string{"ollama", "gemini", "groq"} {
		cfg := Default(provider)
		if provider != "ollama" {
			cfg.APIKey = "key"
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid %s config rejected: %v", provider, err)
		}
	}
}

func TestSanitized_RemovesAPIKey(t *testing.T) {
	cfg := Default("groq")
	cfg.APIKey = "super-secret"
	sanitized := cfg.Sanitized()
	if sanitized.APIKey != "" {
		t.Errorf("Sanitized must drop the API key")
	}
	if sanitized.Model != cfg.Model || sanitized.Provider != cfg.Provider {
		t.Errorf("Sanitized changed non-secret fields")
	}
}

func TestBackoffDelay_Bounded(t *testing.T) {
	base := 100 * time.Millisecond
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}
	for i, w := range want {
		if got := BackoffDelay(base, i+1); got != w {
			t.Errorf("BackoffDelay(attempt=%d) = %v, want %v", i+1, got, w)
		}
	}
	// Capped at MaxRetryBackoff no matter how large the attempt is.
	if got := BackoffDelay(base, 50); got != MaxRetryBackoff {
		t.Errorf("BackoffDelay must cap at %v, got %v", MaxRetryBackoff, got)
	}
	// Non-positive attempts are clamped to 1 and base<=0 falls back.
	if got := BackoffDelay(base, 0); got != base {
		t.Errorf("BackoffDelay(attempt=0) = %v, want %v", got, base)
	}
	if got := BackoffDelay(0, 1); got != DefaultRetryBackoff {
		t.Errorf("BackoffDelay(base=0) = %v, want %v", got, DefaultRetryBackoff)
	}
}

func TestValidateBaseURL(t *testing.T) {
	// Every provider may omit its base URL and fall back to documented defaults.
	for _, provider := range []string{"ollama", "gemini", "groq"} {
		if _, err := ValidateBaseURL("", provider); err != nil {
			t.Errorf("%s may omit the base URL: %v", provider, err)
		}
	}
	if _, err := ValidateBaseURL("ftp://example.test", "ollama"); err == nil {
		t.Errorf("non-http scheme must be rejected")
	}
	if u, err := ValidateBaseURL("http://localhost:11434/v1", "ollama"); err != nil || u.Host == "" {
		t.Errorf("valid URL rejected: %v", err)
	}
}

func TestListProviders_Deterministic(t *testing.T) {
	first := strings.Join(ListProviders(), ",")
	for i := 0; i < 20; i++ {
		if got := strings.Join(ListProviders(), ","); got != first {
			t.Fatalf("ListProviders is not deterministic: %q vs %q", first, got)
		}
	}
	if first != "gemini,groq,ollama" {
		t.Errorf("unexpected provider registry order/content: %q", first)
	}
}
