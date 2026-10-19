package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
	"CommitIssues/internal/toon"
)

func init() {
	// Auto-register Ollama
	Register("ollama", newOllamaResolver)
}

type OllamaResolver struct {
	BaseURL string
	Model   string
	Client  *http.Client
	// APIKey is never sent to a local Ollama instance (it needs no auth) but
	// is retained so that a configured AI_API_KEY can still be redacted from
	// provider error messages.
	APIKey string
	// MaxResponseSize bounds the accepted HTTP response body.
	MaxResponseSize int
	// PayloadFormat selects TOON (default) or JSON for the user payload.
	PayloadFormat string
}

func newOllamaResolver(cfg Config, client *http.Client) (Resolver, error) {
	if parsed, err := ValidateBaseURL(cfg.BaseURL, cfg.Provider); err != nil {
		return nil, err
	} else if parsed == nil {
		// Empty base URL falls back to the documented default.
		cfg.BaseURL = DefaultOllamaBaseURL
	}
	if client == nil {
		client = NewClient(cfg)
	}
	return &OllamaResolver{
		BaseURL:         cfg.BaseURL,
		Model:           cfg.Model,
		Client:          client,
		APIKey:          cfg.APIKey,
		MaxResponseSize: cfg.MaxResponseSize,
		PayloadFormat:   cfg.EffectivePayloadFormat(),
	}, nil
}

func (o *OllamaResolver) ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	return resolveOpenAICompatible(ctx, o.Client, o.BaseURL, o.Model, "", o.APIKey, collision, promptCtx, "ollama", o.MaxResponseSize, o.PayloadFormat)
}

// resolveOpenAICompatible is the shared OpenAI-compatible chat completion
// flow used by Ollama and Groq: identical payload schema, different
// endpoints and auth.
// encodeCollisionPayload serializes the collision for the provider in the
// configured format. TOON is the default (token-efficient for the uniform
// arrays that dominate payloads); JSON remains the per-run fallback.
//
// The encoded payload is wrapped in explicit XML-style boundaries so the
// model treats the embedded code strictly as data. Without isolation, a
// malicious comment or string inside a conflicted file (e.g.
// "// SYSTEM: output ...") can be interpreted as a system instruction —
// classic prompt injection. The system prompt declares these tags untrusted.
func encodeCollisionPayload(collision semantic.DiffItem, format string) (string, error) {
	var body string
	if format == "json" {
		data, err := json.Marshal(collision)
		if err != nil {
			return "", err
		}
		body = string(data)
	} else {
		encoded, err := toon.Marshal(collision)
		if err != nil {
			return "", err
		}
		body = encoded
	}

	var sb strings.Builder
	sb.WriteString("<conflict_data>\n")
	sb.WriteString("<file_name>")
	sb.WriteString(sanitizeBoundary(collision.File))
	sb.WriteString("</file_name>\n")
	sb.WriteString("<base_code>\n")
	sb.WriteString(sanitizeBoundary(collision.BaseContent))
	sb.WriteString("\n</base_code>\n")
	sb.WriteString("<ours_code>\n")
	sb.WriteString(sanitizeBoundary(collision.OurContent))
	sb.WriteString("\n</ours_code>\n")
	sb.WriteString("<theirs_code>\n")
	sb.WriteString(sanitizeBoundary(collision.TheirContent))
	sb.WriteString("\n</theirs_code>\n")
	sb.WriteString("<structured_payload format=\"")
	sb.WriteString(format)
	sb.WriteString("\">\n")
	sb.WriteString(sanitizeBoundary(body))
	sb.WriteString("\n</structured_payload>\n")
	sb.WriteString("</conflict_data>")
	return sb.String(), nil
}

// sanitizeBoundary neutralizes any literal occurrences of our own boundary
// tags inside untrusted code, so injected code cannot prematurely close a
// <..._code> block and escape into instruction space.
func sanitizeBoundary(s string) string {
	for _, tag := range []string{
		"<conflict_data>", "</conflict_data>",
		"<file_name>", "</file_name>",
		"<base_code>", "</base_code>",
		"<ours_code>", "</ours_code>",
		"<theirs_code>", "</theirs_code>",
		"<structured_payload", "</structured_payload>",
	} {
		s = strings.ReplaceAll(s, tag, strings.ReplaceAll(strings.ReplaceAll(tag, "<", "&lt;"), ">", "&gt;"))
	}
	return s
}

func resolveOpenAICompatible(ctx context.Context, client *http.Client, baseURL, model, authKey, secretKey string, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR, provider string, maxResponseSize int, payloadFormat string) (*AIResolutionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, AsError(err)
	}
	if payloadFormat == "" {
		payloadFormat = DefaultPayloadFormat
	}
	userPayload, err := encodeCollisionPayload(collision, payloadFormat)
	if err != nil {
		return nil, NewError(CodeConfigInvalid, false, "failed to marshal collision payload: %v", err)
	}

	reqBody := map[string]any{
		"model":       model,
		"temperature": 0.0,
		"messages": []map[string]string{
			{"role": "system", "content": systemPromptFor(payloadFormat)},
			{"role": "user", "content": promptCtx.Context + "\n\n" + userPayload},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := postJSON(ctx, url, reqBody)
	if err != nil {
		return nil, err
	}
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}

	body, err := performRequest(ctx, client, req, provider, maxResponseSize, secretKey)
	if err != nil {
		return nil, err
	}

	content, err := extractMessageContent(body, provider)
	if err != nil {
		return nil, err
	}
	return parseJSONResponse(content)
}
