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
	// Auto-register Ollama
	Register("ollama", newOllamaResolver)
}

type OllamaResolver struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

func newOllamaResolver(cfg AIConfig, client *http.Client) (Resolver, error) {
	url := cfg.BaseURL
	if url == "" {
		url = "http://localhost:11434/v1" // Standard local Ollama port
	}
	model := cfg.Model
	if model == "" {
		model = "llama3"
	}
	return &OllamaResolver{BaseURL: url, Model: model, Client: client}, nil
}

func (o *OllamaResolver) ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	userPayload, _ := json.Marshal(collision)

	// Ollama uses the exact same JSON format as OpenAI
	reqBody := map[string]any{
		"model":       o.Model,
		"temperature": 0.0,
		"messages": []map[string]string{
			{"role": "system", "content": defaultSystemPrompt},
			{"role": "user", "content": promptCtx.Context + "\n\n" + string(userPayload)},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	url := strings.TrimRight(o.BaseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	// Notice: No API key header needed for local Ollama!

	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama error (%d): %s", resp.StatusCode, string(respBytes))
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
		return nil, fmt.Errorf("ollama returned no choices")
	}

	return parseJSONResponse(parsed.Choices[0].Message.Content)
}