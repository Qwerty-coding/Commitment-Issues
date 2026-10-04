package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

// collisionFixture is a representative structural collision payload.
func collisionFixture() semantic.DiffItem {
	return semantic.DiffItem{
		Type:         "COLLISION",
		Kind:         "Function",
		Name:         "calculate",
		Line:         3,
		BaseContent:  "return x + 1",
		OurContent:   "return x + 2",
		TheirContent: "return x * 2",
		File:         "conflict.js",
	}
}

// captureBody serves one request and records the raw request body.
func captureBody(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	bodies := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(data))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"explanation\":\"ok\",\"suggested_code\":\"x\",\"confidence_score\":90}"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, bodies
}

func TestResolveOpenAICompatible_ToonPayload(t *testing.T) {
	srv, bodies := captureBody(t)
	resolver, err := newOllamaResolver(Config{
		Provider:        "ollama",
		Model:           "test-model",
		BaseURL:         srv.URL,
		MaxResponseSize: 1024 * 1024,
		PayloadFormat:   "toon",
	}, srv.Client())
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}

	if _, err := resolver.ResolveCollision(context.Background(), collisionFixture(), promptcontext.PromptContextIR{}); err != nil {
		t.Fatalf("ResolveCollision: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*bodies))
	}

	body := (*bodies)[0]
	// The request body is JSON, so TOON newlines appear escaped.
	if !strings.Contains(body, "\\nkind: Function\\n") {
		t.Errorf("TOON payload should contain key: value lines, body:\n%s", body)
	}
	if !strings.Contains(body, toonPayloadNote) {
		t.Errorf("system prompt should explain the TOON format, body:\n%s", body)
	}
	if strings.Contains(body, `"kind":"Function"`) {
		t.Errorf("TOON payload must not be JSON, body:\n%s", body)
	}
}

func TestResolveOpenAICompatible_JsonPayload(t *testing.T) {
	srv, bodies := captureBody(t)
	resolver, err := newOllamaResolver(Config{
		Provider:        "ollama",
		Model:           "test-model",
		BaseURL:         srv.URL,
		MaxResponseSize: 1024 * 1024,
		PayloadFormat:   "json",
	}, srv.Client())
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}

	if _, err := resolver.ResolveCollision(context.Background(), collisionFixture(), promptcontext.PromptContextIR{}); err != nil {
		t.Fatalf("ResolveCollision: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*bodies))
	}

	body := (*bodies)[0]
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("request body is not valid JSON: %v\n%s", err, body)
	}
	if len(parsed.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(parsed.Messages))
	}

	var collision semantic.DiffItem
	if err := json.Unmarshal([]byte(parsed.Messages[1].Content), &collision); err != nil {
		t.Fatalf("user payload is not valid JSON: %v\n%s", err, parsed.Messages[1].Content)
	}
	if collision.Kind != "Function" || collision.Name != "calculate" {
		t.Errorf("unexpected collision payload: %+v", collision)
	}
	if strings.Contains(parsed.Messages[0].Content, toonPayloadNote) {
		t.Error("JSON payloads must not carry the TOON explainer line")
	}
}

func TestConfig_ValidatePayloadFormat(t *testing.T) {
	cfg := Default("")
	cfg.BaseURL = "http://127.0.0.1:1"
	cfg.PayloadFormat = "yaml"
	if err := cfg.Validate(); err == nil {
		t.Error("invalid payload format should fail validation")
	}
	cfg.PayloadFormat = "json"
	if err := cfg.Validate(); err != nil {
		t.Errorf("json payload format should validate: %v", err)
	}
}
