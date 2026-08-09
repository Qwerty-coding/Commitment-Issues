package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

func init() {
	// Auto-register Gemini when this file is compiled
	Register("gemini", newGeminiResolver)
}

type GeminiResolver struct {
	APIKey string
	Model  string
	Client *http.Client
}

func newGeminiResolver(cfg AIConfig, client *http.Client) (Resolver, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gemini requires an API key")
	}
	model := cfg.Model
	if model == "" {
		model = "gemini-3.5-flash"
	}
	return &GeminiResolver{APIKey: cfg.APIKey, Model: model, Client: client}, nil
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
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", g.Model, g.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini API error (%d): %s", resp.StatusCode, string(respBytes))
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
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini returned empty response")
	}

	return parseJSONResponse(parsed.Candidates[0].Content.Parts[0].Text)
}
