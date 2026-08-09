package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

// AIResolutionResponse is the uniform response returned by ALL providers
type AIResolutionResponse struct {
	Explanation   string `json:"explanation"`
	SuggestedCode string `json:"suggested_code"`
	Confidence    int    `json:"confidence_score"`
}

// Resolver is the contract that every AI provider must implement
type Resolver interface {
	ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error)
}

// AIConfig holds the runtime options supplied by the user/CLI
type AIConfig struct {
	Provider string // e.g. "gemini", "ollama", "groq", "openai", "claude"
	Model    string // e.g. "gemini-1.5-flash", "llama3", "qwen2.5-coder"
	APIKey   string
	BaseURL  string // Custom endpoint URL
}

// FactoryFunc is a constructor function signature for creating a Resolver
type FactoryFunc func(cfg AIConfig, client *http.Client) (Resolver, error)

// Standard system prompt used across all providers
const defaultSystemPrompt = `You are an expert code resolution engine. You resolve Git merge conflicts strictly at the structural node level. You will be provided with three versions of a single function or variable: Base, Ours, and Theirs.

Resolution strategy:
1. If the two changes are compatible, merge them together.
2. If they are mutually exclusive, prefer the simplest solution that avoids duplicated logic and clearly states the trade-off in the explanation.
3. Preserve the intent of both branches wherever possible.
4. Rate confidence (0-100) based on how certain you are in the merge strategy.

Output ONLY valid JSON matching this schema: {"explanation": "string", "suggested_code": "string", "confidence_score": int}.`

// ============================================================================
// DYNAMIC PROVIDER REGISTRY
// ============================================================================

var (
	registryMutex sync.RWMutex
	providers     = make(map[string]FactoryFunc)
)

// Register makes an AI provider available to the application
func Register(name string, factory FactoryFunc) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	providers[strings.ToLower(name)] = factory
}

// ListProviders returns a list of all currently registered AI provider names
func ListProviders() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	list := make([]string, 0, len(providers))
	for name := range providers {
		list = append(list, name)
	}
	return list
}

// GetResolver instantiates the selected AI provider based on cfg.Provider
func GetResolver(cfg AIConfig) (Resolver, error) {
	registryMutex.RLock()
	factory, exists := providers[strings.ToLower(cfg.Provider)]
	registryMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unsupported AI provider '%s'. Available providers: %s",
			cfg.Provider, strings.Join(ListProviders(), ", "))
	}

	client := &http.Client{Timeout: 60 * time.Second}
	return factory(cfg, client)
}

// ============================================================================
// SHARED UTILITIES
// ============================================================================

func parseJSONResponse(rawText string) (*AIResolutionResponse, error) {
	rawText = strings.TrimSpace(rawText)
	rawText = strings.TrimPrefix(rawText, "```json")
	rawText = strings.TrimPrefix(rawText, "```")
	rawText = strings.TrimSuffix(rawText, "```")
	rawText = strings.TrimSpace(rawText)

	var resolution AIResolutionResponse
	if err := json.Unmarshal([]byte(rawText), &resolution); err != nil {
		return nil, fmt.Errorf("failed to parse AI resolution JSON: %w (raw: %s)", err, rawText)
	}
	return &resolution, nil
}