package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

func init() {
	// Auto-register Groq when this file is compiled
	Register("groq", newGroqResolver)
}

type GroqResolver struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
}

func newGroqResolver(cfg AIConfig, client *http.Client) (Resolver, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("groq requires an API key")
	}
	
	model := cfg.Model
	if model == "" {
		// Default to Groq's fast Llama 3 model if none is provided
		model = "llama-3.3-70b-versatile"
	}
	
	return &GroqResolver{
		BaseURL: "https://api.groq.com/openai/v1", 
		Model:   model, 
		APIKey:  cfg.APIKey, 
		Client:  client,
	}, nil
}

func (g *GroqResolver) ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	userPayload, _ := json.Marshal(collision)

	// Groq uses the exact same payload schema as OpenAI
	reqBody := map[string]any{
		"model":       g.Model,
		"temperature": 0.0,
		"messages": []map[string]string{
			{"role": "system", "content": defaultSystemPrompt},
			{"role": "user", "content": promptCtx.Context + "\n\n" + string(userPayload)},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	url := strings.TrimRight(g.BaseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.APIKey)

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("groq API error (%d): %s", resp.StatusCode, string(respBytes))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("groq returned no choices")
	}

	return parseJSONResponse(parsed.Choices[0].Message.Content)
}