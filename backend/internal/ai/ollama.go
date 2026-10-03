package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
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
	}, nil
}

func (o *OllamaResolver) ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	return resolveOpenAICompatible(ctx, o.Client, o.BaseURL, o.Model, "", o.APIKey, collision, promptCtx, "ollama", o.MaxResponseSize)
}

// resolveOpenAICompatible is the shared OpenAI-compatible chat completion
// flow used by Ollama and Groq: identical payload schema, different
// endpoints and auth.
func resolveOpenAICompatible(ctx context.Context, client *http.Client, baseURL, model, authKey, secretKey string, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR, provider string, maxResponseSize int) (*AIResolutionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, AsError(err)
	}
	userPayload, err := json.Marshal(collision)
	if err != nil {
		return nil, NewError(CodeConfigInvalid, false, "failed to marshal collision payload: %v", err)
	}

	reqBody := map[string]any{
		"model":       model,
		"temperature": 0.0,
		"messages": []map[string]string{
			{"role": "system", "content": defaultSystemPrompt},
			{"role": "user", "content": promptCtx.Context + "\n\n" + string(userPayload)},
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
