// ai contains the provider abstraction and fallback logic used to resolve merge conflicts.
package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Provider defines the minimal interface required for code-resolution backends.
type Provider interface {
	Resolve(payload ConflictPayload, fullLocalCode string) (string, error)
}

// providerFactory builds the active provider based on configuration.
type providerFactory func() (Provider, error)

var activeProviderFactory providerFactory = func() (Provider, error) {
	providerName := strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER")))
	if providerName == "" || providerName == "gemini" {
		return NewGeminiProvider()
	}
	if providerName == "qwen" {
		return NewOllamaProvider(defaultQwenModel)
	}
	if providerName == "llama" {
		return NewOllamaProvider(defaultLlamaModel)
	}
	return nil, fmt.Errorf("unsupported AI provider: %s", providerName)
}

// SetProviderFactory allows the app to swap in another provider later.
func SetProviderFactory(factory providerFactory) {
	activeProviderFactory = factory
}

// ResolveWithProvider uses the configured provider to resolve the conflict payload.
func ResolveWithProvider(payload ConflictPayload, fullLocalCode string) (string, error) {
	provider, err := activeProviderFactory()
	if err != nil {
		return "", fmt.Errorf("create provider: %w", err)
	}
	resolved, providerErr := provider.Resolve(payload, fullLocalCode)
	if providerErr != nil {
		fallback, fallbackErr := fallbackResolve(payload, fullLocalCode)
		if fallbackErr != nil {
			return "", fmt.Errorf("provider failed: %w; fallback failed: %v", providerErr, fallbackErr)
		}
		return fallback, nil
	}
	return resolved, nil
}

func fallbackResolve(payload ConflictPayload, fullLocalCode string) (string, error) {
	if len(payload.Operations) == 0 {
		return fullLocalCode, nil
	}

	mergedCode := fullLocalCode
	for _, operation := range payload.Operations {
		selectedSnippet := selectFallbackSnippet(operation)
		if selectedSnippet == "" {
			continue
		}

		if strings.Contains(mergedCode, operation.LocalCode) {
			mergedCode = strings.ReplaceAll(mergedCode, operation.LocalCode, selectedSnippet)
			continue
		}

		if operation.RemoteCode != "" && strings.Contains(mergedCode, operation.RemoteCode) {
			mergedCode = strings.ReplaceAll(mergedCode, operation.RemoteCode, selectedSnippet)
		}
	}

	if mergedCode == fullLocalCode {
		return fullLocalCode, nil
	}
	return mergedCode, nil
}

func selectFallbackSnippet(operation ASTOperation) string {
	switch {
	case operation.LocalCode == "" && operation.RemoteCode != "":
		return operation.RemoteCode
	case operation.RemoteCode == "" && operation.LocalCode != "":
		return operation.LocalCode
	case operation.LocalCode == operation.RemoteCode:
		return operation.LocalCode
	case operation.Action == "DELETE":
		return ""
	case operation.Action == "INSERT":
		if operation.LocalCode != "" {
			return operation.LocalCode
		}
		return operation.RemoteCode
	default:
		return operation.RemoteCode
	}
}

const (
	defaultQwenModel  = "qwen2.5-coder"
	defaultLlamaModel = "llama3.1"
	ollamaDefaultHost = "http://localhost:11434"
)

type ollamaProvider struct {
	model  string
	host   string
	client *http.Client
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []ollamaMessage `json:"messages"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
	Error   string        `json:"error"`
}

// NewOllamaProvider creates a provider backed by a local Ollama server.
func NewOllamaProvider(model string) (Provider, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("ollama model name is required")
	}

	host := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if host == "" {
		host = ollamaDefaultHost
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}

	return &ollamaProvider{
		model: model,
		host:  strings.TrimRight(host, "/"),
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}, nil
}

func (p *ollamaProvider) Resolve(payload ConflictPayload, fullLocalCode string) (string, error) {
	prompt, err := buildOllamaPrompt(payload, fullLocalCode)
	if err != nil {
		return "", err
	}

	requestBody, err := json.Marshal(ollamaChatRequest{
		Model:  p.model,
		Stream: false,
		Messages: []ollamaMessage{{
			Role:    "system",
			Content: "You resolve merge conflicts. Return only valid merged source code with no explanation.",
		}, {
			Role:    "user",
			Content: prompt,
		}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal ollama request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, p.host+"/api/chat", bytes.NewReader(requestBody))
	if err != nil {
		return "", fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("ollama returned status %s", resp.Status)
	}

	var chatResponse ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResponse); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}
	if strings.TrimSpace(chatResponse.Error) != "" {
		return "", fmt.Errorf("ollama returned error: %s", strings.TrimSpace(chatResponse.Error))
	}

	resolvedCode := strings.TrimSpace(chatResponse.Message.Content)
	resolvedCode = strings.ReplaceAll(resolvedCode, "```toon", "")
	resolvedCode = strings.ReplaceAll(resolvedCode, "```", "")
	resolvedCode = strings.TrimSpace(resolvedCode)
	if resolvedCode == "" {
		return "", fmt.Errorf("ollama returned an empty candidate body")
	}

	return resolvedCode, nil
}

func buildOllamaPrompt(payload ConflictPayload, fullLocalCode string) (string, error) {
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal conflict payload: %w", err)
	}

	return fmt.Sprintf(`You are an expert compiler engineer resolving Git merge conflicts.
Return only the valid merged source code for the file.

=== CONFLICT PAYLOAD ===
%s

=== FULL LOCAL CONTEXT ===
%s`, string(payloadJSON), fullLocalCode), nil
}
