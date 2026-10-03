package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

func init() {
	// Auto-register Gemini when this file is compiled
	Register("gemini", newGeminiResolver)
}

// GeminiResolver resolves collisions through Google's Generative Language
// API. The API key is passed via the x-goog-api-key header so that the key
// can never leak into URLs or error messages.
type GeminiResolver struct {
	APIKey string
	Model  string
	Client *http.Client
	// BaseURL is the Generative Language API root; it defaults to the
	// documented Google endpoint and is overridable via AI_BASE_URL.
	BaseURL string
	// MaxResponseSize bounds the accepted HTTP response body.
	MaxResponseSize int
}

func newGeminiResolver(cfg Config, client *http.Client) (Resolver, error) {
	if cfg.APIKey == "" {
		return nil, NewError(CodeAPIKeyMissing, false, "gemini requires an API key")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = geminiAPIBaseURL
	}
	if _, err := ValidateBaseURL(baseURL, cfg.Provider); err != nil {
		return nil, err
	}
	if client == nil {
		client = NewClient(cfg)
	}
	return &GeminiResolver{
		APIKey:          cfg.APIKey,
		Model:           cfg.Model,
		Client:          client,
		BaseURL:         strings.TrimRight(baseURL, "/"),
		MaxResponseSize: cfg.MaxResponseSize,
	}, nil
}

func (g *GeminiResolver) ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	userPayload, _ := json.Marshal(collision)

	reqBody := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": defaultSystemPrompt}}},
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": promptCtx.Context}}},
			{"role": "user", "parts": []map[string]string{{"text": string(userPayload)}}},
		},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"temperature":      0.0,
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	// The key travels in a header, not the query string, so transport errors
	// (url.Error embeds the URL) can never carry the credential.
	url := g.BaseURL + "/models/" + g.Model + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, NewError(CodeConfigInvalid, false, "%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)

	body, err := performRequest(ctx, g.Client, req, "gemini", g.MaxResponseSize, g.APIKey)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, WrapError(CodeMalformedJSON, false, err, "failed to parse gemini response")
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return nil, NewError(CodeEmptyResponse, false, "gemini returned empty response")
	}

	return parseJSONResponse(parsed.Candidates[0].Content.Parts[0].Text)
}
