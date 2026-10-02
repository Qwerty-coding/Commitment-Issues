package engine

import (
	"errors"
	"testing"
	"time"
)

func TestConfigValidate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got %v", err)
	}
}

func TestConfigValidate_MaxConcurrency(t *testing.T) {
	for _, value := range []int{0, -1, -100} {
		cfg := Config{MaxConcurrency: value, Timeout: time.Second, ConfidenceThreshold: 50}
		err := cfg.Validate()
		if err == nil {
			t.Fatalf("MaxConcurrency=%d should be invalid", value)
		}
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) {
			t.Fatalf("expected *ConfigError, got %T", err)
		}
		if cfgErr.Field != "MaxConcurrency" {
			t.Errorf("field = %q, want MaxConcurrency", cfgErr.Field)
		}
		if cfgErr.Code() != CodeInvalidConfig {
			t.Errorf("code = %q, want %q", cfgErr.Code(), CodeInvalidConfig)
		}
	}
}

func TestConfigValidate_Timeout(t *testing.T) {
	for _, value := range []time.Duration{0, -time.Second} {
		cfg := Config{MaxConcurrency: 1, Timeout: value, ConfidenceThreshold: 50}
		err := cfg.Validate()
		if err == nil {
			t.Fatalf("Timeout=%s should be invalid", value)
		}
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) {
			t.Fatalf("expected *ConfigError, got %T", err)
		}
		if cfgErr.Field != "Timeout" {
			t.Errorf("field = %q, want Timeout", cfgErr.Field)
		}
	}
}

func TestConfigValidate_ConfidenceThreshold(t *testing.T) {
	for _, value := range []int{-1, 101, 1000} {
		cfg := Config{MaxConcurrency: 1, Timeout: time.Second, ConfidenceThreshold: value}
		err := cfg.Validate()
		if err == nil {
			t.Fatalf("ConfidenceThreshold=%d should be invalid", value)
		}
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) {
			t.Fatalf("expected *ConfigError, got %T", err)
		}
		if cfgErr.Field != "ConfidenceThreshold" {
			t.Errorf("field = %q, want ConfidenceThreshold", cfgErr.Field)
		}
	}
}

func TestConfigValidate_BoundaryValuesAccepted(t *testing.T) {
	cases := []Config{
		{MaxConcurrency: 1, Timeout: time.Nanosecond, ConfidenceThreshold: 0},
		{MaxConcurrency: 1, Timeout: time.Nanosecond, ConfidenceThreshold: 100},
	}
	for _, cfg := range cases {
		if err := cfg.Validate(); err != nil {
			t.Errorf("boundary config %+v should be valid, got %v", cfg, err)
		}
	}
}

func TestConfigValidate_DeterministicFirstError(t *testing.T) {
	// Several fields are invalid at once; MaxConcurrency must always be
	// reported first so validation is deterministic and testable.
	cfg := Config{MaxConcurrency: 0, Timeout: 0, ConfidenceThreshold: 999}
	var cfgErr *ConfigError
	if err := cfg.Validate(); !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigError")
	}
	if cfgErr.Field != "MaxConcurrency" {
		t.Errorf("first reported field = %q, want MaxConcurrency", cfgErr.Field)
	}
}

func TestErrorCodeOf(t *testing.T) {
	if got := ErrorCodeOf(nil); got != "" {
		t.Errorf("nil error code = %q, want empty", got)
	}
	if got := ErrorCodeOf(&ConfigError{Field: "x"}); got != CodeInvalidConfig {
		t.Errorf("config error code = %q", got)
	}
	if got := ErrorCodeOf(errors.New("plain")); got != CodeInternal {
		t.Errorf("plain error code = %q, want %q", got, CodeInternal)
	}
	if got := ErrorCodeOf(&FileError{File: "f.go", ErrCode: CodeFileParse}); got != CodeFileParse {
		t.Errorf("file error code = %q", got)
	}
}
