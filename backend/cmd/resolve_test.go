package cmd

import (
	"testing"

	ai "CommitIssues/internal/ai"
)

// clearAIEnv isolates the tests from ambient AI_* configuration so
// the CLI flag path is exercised deterministically.
func clearAIEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"AI_PROVIDER", "AI_MODEL", "AI_BASE_URL", "AI_API_KEY",
		"AI_TIMEOUT", "AI_RETRIES", "AI_CONFIDENCE_THRESHOLD",
		"AI_MAX_PROMPT_BYTES", "AI_MAX_RESPONSE_BYTES",
		"AI_RETRY_BACKOFF", "OLLAMA_BASE_URL",
	} {
		t.Setenv(k, "")
	}
}

func setFlag(t *testing.T, name, value string) {
	t.Helper()
	if err := resolveCmd.Flags().Set(name, value); err != nil {
		t.Fatalf("cannot set --%s: %v", name, err)
	}
	// Cobra keeps flag state for the lifetime of the process.
	// Reset the Changed marker after the test so flags set in
	// one test never leak into another.
	t.Cleanup(func() {
		if f := resolveCmd.Flags().Lookup(name); f != nil {
			f.Changed = false
		}
	})
}

// TestBuildAIConfigFromFlags_ProviderAloneWorks reproduces the
// regression where `--provider gemini` (without AI_PROVIDER in the
// environment) kept the Ollama fallback model and base URL. Every
// provider must work from the flag alone.
func TestBuildAIConfigFromFlags_ProviderAloneWorks(t *testing.T) {
	clearAIEnv(t)

	cases := []struct {
		provider string
		model    string
		baseURL  string
	}{
		{"gemini", ai.DefaultGeminiModel, ""},
		{"groq", ai.DefaultGroqModel, ai.DefaultGroqBaseURL},
		{"ollama", ai.DefaultOllamaModel, ai.DefaultOllamaBaseURL},
		{"GEMINI", ai.DefaultGeminiModel, ""}, // normalized
	}
	for _, tc := range cases {
		setFlag(t, "provider", tc.provider)
		cfg := buildAIConfigFromFlags(resolveCmd)
		if cfg.Provider != tc.provider && cfg.Provider != lower(tc.provider) {
			t.Errorf("provider = %q, want %q (normalized)", cfg.Provider, tc.provider)
		}
		if cfg.Model != tc.model {
			t.Errorf("--provider %s: model = %q, want %q", tc.provider, cfg.Model, tc.model)
		}
		if cfg.BaseURL != tc.baseURL {
			t.Errorf("--provider %s: base URL = %q, want %q", tc.provider, cfg.BaseURL, tc.baseURL)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("--provider %s: config must validate: %v", tc.provider, err)
		}
	}
}

func lower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// TestBuildAIConfigFromFlags_ExplicitModelWins verifies the
// precedence: an explicit --model beats the provider default.
func TestBuildAIConfigFromFlags_ExplicitModelWins(t *testing.T) {
	clearAIEnv(t)
	setFlag(t, "provider", "gemini")
	setFlag(t, "model", "my-custom-model")

	cfg := buildAIConfigFromFlags(resolveCmd)
	if cfg.Model != "my-custom-model" {
		t.Errorf("--model must win over the provider default, got %q", cfg.Model)
	}
	if cfg.Provider != "gemini" {
		t.Errorf("provider = %q, want gemini", cfg.Provider)
	}
}

// TestBuildAIConfigFromFlags_EnvModelWins verifies the precedence:
// an AI_MODEL environment variable beats the provider default but
// loses to an explicit flag.
func TestBuildAIConfigFromFlags_EnvModelWins(t *testing.T) {
	clearAIEnv(t)
	t.Setenv("AI_MODEL", "env-model")
	setFlag(t, "provider", "groq")

	cfg := buildAIConfigFromFlags(resolveCmd)
	if cfg.Model != "env-model" {
		t.Errorf("AI_MODEL must win over the provider default, got %q", cfg.Model)
	}

	setFlag(t, "model", "flag-model")
	cfg = buildAIConfigFromFlags(resolveCmd)
	if cfg.Model != "flag-model" {
		t.Errorf("--model must win over AI_MODEL, got %q", cfg.Model)
	}
}

// TestBuildAIConfigFromFlags_NoProviderFlagKeepsEnvBehavior verifies
// that without --provider the previous environment-driven behavior is
// unchanged.
func TestBuildAIConfigFromFlags_NoProviderFlagKeepsEnvBehavior(t *testing.T) {
	clearAIEnv(t)
	t.Setenv("AI_PROVIDER", "groq")

	cfg := buildAIConfigFromFlags(resolveCmd)
	if cfg.Provider != "groq" || cfg.Model != ai.DefaultGroqModel {
		t.Errorf("env-driven config changed: %+v", cfg)
	}
}
