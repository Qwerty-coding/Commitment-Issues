package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func tagsServer(t *testing.T, models []string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		var items []string
		for _, m := range models {
			items = append(items, fmt.Sprintf(`{"name":%q}`, m))
		}
		_, _ = w.Write([]byte(`{"models":[` + strings.Join(items, ",") + `]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckProvider_OllamaReachableWithModel(t *testing.T) {
	srv := tagsServer(t, []string{"llama3:latest", "qwen2.5-coder:7b"}, http.StatusOK)
	cfg := Default("")
	cfg.BaseURL = srv.URL + "/v1"
	if err := CheckProvider(context.Background(), cfg, nil); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}

func TestCheckProvider_OllamaModelTagNormalization(t *testing.T) {
	srv := tagsServer(t, []string{"qwen2:latest"}, http.StatusOK)
	cfg := Default("")
	cfg.BaseURL = srv.URL + "/v1"
	cfg.Model = "qwen2" // untagged must match :latest
	if err := CheckProvider(context.Background(), cfg, nil); err != nil {
		t.Fatalf("untagged model should match :latest: %v", err)
	}
}

func TestCheckProvider_OllamaModelMissing(t *testing.T) {
	srv := tagsServer(t, []string{"llama3:latest"}, http.StatusOK)
	cfg := Default("")
	cfg.BaseURL = srv.URL + "/v1"
	err := CheckProvider(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected a missing-model error")
	}
	typed := AsError(err)
	if typed.Code != CodeModelMissing {
		t.Errorf("code = %q, want %q", typed.Code, CodeModelMissing)
	}
	if typed.Retryable {
		t.Errorf("a missing model is not transient")
	}
	if !strings.Contains(typed.Error(), "llama3:latest") {
		t.Errorf("error should list installed models: %s", typed.Error())
	}
}

func TestCheckProvider_OllamaUnreachable(t *testing.T) {
	// Reserve a port and close it so the endpoint is guaranteed unreachable.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()

	cfg := Default("")
	cfg.BaseURL = addr + "/v1"
	cfg.RequestTimeout = 500 * time.Millisecond
	err := CheckProvider(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected an unreachable-provider error")
	}
	typed := AsError(err)
	if typed.Code != CodeProviderUnavailable {
		t.Errorf("code = %q, want %q", typed.Code, CodeProviderUnavailable)
	}
	if !typed.Retryable {
		t.Errorf("an unreachable provider is transient and should be retryable")
	}
}

func TestCheckProvider_OllamaNon200(t *testing.T) {
	srv := tagsServer(t, nil, http.StatusInternalServerError)
	cfg := Default("")
	cfg.BaseURL = srv.URL + "/v1"
	err := CheckProvider(context.Background(), cfg, nil)
	if err == nil || AsError(err).Code != CodeProviderUnavailable {
		t.Fatalf("expected provider-unavailable, got %v", err)
	}
}

func TestCheckProvider_OllamaMalformedTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	cfg := Default("")
	cfg.BaseURL = srv.URL + "/v1"
	err := CheckProvider(context.Background(), cfg, nil)
	if err == nil || AsError(err).Code != CodeMalformedJSON {
		t.Fatalf("expected malformed-json, got %v", err)
	}
}

func TestCheckProvider_MissingAPIKey(t *testing.T) {
	for _, provider := range []string{"gemini", "groq"} {
		cfg := Default(provider)
		cfg.APIKey = ""
		err := CheckProvider(context.Background(), cfg, nil)
		if err == nil {
			t.Fatalf("%s without a key must fail", provider)
		}
		typed := AsError(err)
		if typed.Code != CodeAPIKeyMissing {
			t.Errorf("%s code = %q, want %q", provider, typed.Code, CodeAPIKeyMissing)
		}
		// The error must be actionable without leaking anything.
		if strings.Contains(typed.Error(), "AI_API_KEY=") {
			t.Errorf("error should not echo secret-looking values: %s", typed.Error())
		}
	}
	// With a key present, no provider-specific health probe is needed.
	cfg := Default("gemini")
	cfg.APIKey = "present"
	if err := CheckProvider(context.Background(), cfg, nil); err != nil {
		t.Errorf("gemini with a key should pass: %v", err)
	}
}

func TestCheckProvider_UnsupportedAndInvalidConfig(t *testing.T) {
	cfg := Default("")
	cfg.Provider = "does-not-exist"
	err := CheckProvider(context.Background(), cfg, nil)
	if err == nil || AsError(err).Code != CodeConfigInvalid {
		t.Fatalf("unsupported provider should be a config error: %v", err)
	}

	cfg = Default("")
	cfg.RequestTimeout = 0
	err = CheckProvider(context.Background(), cfg, nil)
	if err == nil || AsError(err).Code != CodeConfigInvalid {
		t.Fatalf("invalid config should be a config error: %v", err)
	}
}

func TestCheckProvider_CancelledContext(t *testing.T) {
	srv := tagsServer(t, []string{"qwen2:1.5b"}, http.StatusOK)
	cfg := Default("")
	cfg.BaseURL = srv.URL + "/v1"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckProvider(ctx, cfg, nil); err == nil {
		t.Fatal("expected cancellation error")
	}
}

// ─── Secret redaction ────────────────────────────────────────────────────────

func TestRedact_QueryParameters(t *testing.T) {
	raw := "Post \"https://generativelanguage.googleapis.com/v1beta/models/x:generateContent?key=AIzaSyTOPSECRET\": dial tcp: timeout"
	got := redact(raw)
	if strings.Contains(got, "AIzaSyTOPSECRET") {
		t.Errorf("API key leaked: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected a redaction marker: %s", got)
	}
}

func TestRedact_BearerTokens(t *testing.T) {
	got := redact("Authorization: Bearer gsk_live_totally_secret\nnext line")
	if strings.Contains(got, "gsk_live_totally_secret") {
		t.Errorf("bearer token leaked: %s", got)
	}
	if !strings.Contains(got, "next line") {
		t.Errorf("unrelated lines must be preserved: %s", got)
	}
}

func TestRedact_OtherParamsAndCase(t *testing.T) {
	got := redact("https://x.test/p?api_key=abc123&other=1")
	if strings.Contains(got, "abc123") {
		t.Errorf("api_key leaked: %s", got)
	}
	if !strings.Contains(got, "other=1") {
		t.Errorf("unrelated params must be preserved: %s", got)
	}
}

func TestAsError_RedactsURLTransportErrors(t *testing.T) {
	_, err := (&http.Client{}).Get("http://127.0.0.1:1/v1/models?key=SUPERSECRET")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	typed := AsError(err)
	if strings.Contains(typed.Error(), "SUPERSECRET") {
		t.Errorf("typed error leaked the key: %s", typed.Error())
	}
}

func TestNewError_Redacts(t *testing.T) {
	err := NewError(CodeHTTPError, true, "failed: %s", "https://x.test/v1?key=SECRET")
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("NewError leaked a secret: %s", err.Error())
	}
}

func TestProviderErrors_DoNotLeakAPIKey(t *testing.T) {
	// A groq-style 401 whose body echoes the key must still be redacted, and
	// a gemini key must never appear in the URL at all.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, "invalid key: %s", "SECRETVALUE")
	}))
	defer srv.Close()

	cfg := Default("groq")
	cfg.BaseURL = srv.URL
	cfg.APIKey = "SECRETVALUE"
	resolver, err := GetResolver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ResolveCollision(context.Background(), diffItem(), promptCtx())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRETVALUE") {
		t.Errorf("provider error leaked the API key: %s", err.Error())
	}
}

func TestGemini_KeyTravelsInHeaderNotURL(t *testing.T) {
	var sawHeader, sawQueryKey, sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("x-goog-api-key")
		sawQueryKey = r.URL.Query().Get("key")
		sawPath = r.URL.Path
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"explanation\":\"e\",\"suggested_code\":\"c\",\"confidence_score\":10}"}]}}]}`))
	}))
	defer srv.Close()

	cfg := Default("gemini")
	cfg.APIKey = "GEMINIKEY"
	cfg.BaseURL = srv.URL
	resolver, err := GetResolver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := resolver.ResolveCollision(context.Background(), diffItem(), promptCtx())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if res.Confidence != 10 {
		t.Errorf("confidence = %d", res.Confidence)
	}
	if sawHeader != "GEMINIKEY" {
		t.Errorf("API key must travel in the x-goog-api-key header, got %q", sawHeader)
	}
	if sawQueryKey != "" {
		t.Errorf("API key must never appear in the URL query, got %q", sawQueryKey)
	}
	if !strings.Contains(sawPath, ":generateContent") {
		t.Errorf("unexpected request path: %q", sawPath)
	}
}

func TestURL_ErrorMessagesRedacted(t *testing.T) {
	// url.Error includes the full URL; ensure the shared redaction covers it.
	u, _ := url.Parse("https://api.test/v1?key=LEAKME")
	raw := (&url.Error{Op: "Post", URL: u.String(), Err: fmt.Errorf("boom")}).Error()
	if !strings.Contains(raw, "LEAKME") {
		t.Skip("url.Error no longer embeds query params; redaction still safe")
	}
	if strings.Contains(redact(raw), "LEAKME") {
		t.Errorf("redact failed for url.Error: %s", redact(raw))
	}
}
