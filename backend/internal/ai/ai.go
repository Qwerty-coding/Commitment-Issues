package ai

import (
	"context"
	"net/http"
	"strings"
	"sync"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

// AIResolutionResponse is the uniform response returned by ALL providers.
// The canonical wire schema is {"explanation", "suggested_code",
// "confidence_score"}; validation is centralized in ValidateResponse.
type AIResolutionResponse struct {
	Explanation   string `json:"explanation"`
	SuggestedCode string `json:"suggested_code"`
	Confidence    int    `json:"confidence_score"`
}

// Resolver is the contract that every AI provider must implement. The
// context is always the caller's request-scoped context: no provider may
// start work on context.Background().
type Resolver interface {
	ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error)
}

// FactoryFunc is a constructor function signature for creating a Resolver.
type FactoryFunc func(cfg Config, client *http.Client) (Resolver, error)

// Standard system prompt used across all providers.
const defaultSystemPrompt = `You are an expert code resolution engine. You resolve Git merge conflicts strictly at the structural node level. You will be provided with three versions of a single function or variable: Base, Ours, and Theirs.

Resolution strategy:
1. If the two changes are compatible, merge them together.
2. If they are mutually exclusive, prefer the simplest solution that avoids duplicated logic and clearly states the trade-off in the explanation.
3. Preserve the intent of both branches wherever possible.
4. Rate confidence (0-100) based on how certain you are in the merge strategy.

Output ONLY valid JSON matching this schema: {"explanation": "string", "suggested_code": "string", "confidence_score": int}.`

// ============================================================================
// DYNAMIC PROVIDER REGISTRY
//
// Write-once, guarded, static configuration (per the Phase 1 audit): this is
// process init data, never run data. Suggestion/report/analysis state remains
// fully run-scoped in internal/runstate.
// ============================================================================

var (
	registryMutex sync.RWMutex
	providers     = make(map[string]FactoryFunc)
)

// Register makes an AI provider available to the application.
func Register(name string, factory FactoryFunc) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	providers[strings.ToLower(name)] = factory
}

// ListProviders returns a sorted list of all currently registered AI provider
// names, so registry enumeration is deterministic.
func ListProviders() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	list := make([]string, 0, len(providers))
	for name := range providers {
		list = append(list, name)
	}
	// Deterministic ordering for error messages and metadata.
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return list
}

// GetResolver instantiates the selected AI provider based on cfg.Provider.
// The HTTP client is bounded by the configured per-request timeout instead of
// a hard-coded value. Callers should run CheckProvider before generating.
func GetResolver(cfg Config) (Resolver, error) {
	registryMutex.RLock()
	factory, exists := providers[strings.ToLower(cfg.Provider)]
	registryMutex.RUnlock()

	if !exists {
		return nil, NewError(CodeProviderUnsupported, false,
			"unsupported AI provider '%s'. Available providers: %s",
			cfg.Provider, strings.Join(ListProviders(), ", "))
	}
	if err := cfg.Validate(); err != nil {
		return nil, NewError(CodeConfigInvalid, false, "%v", err)
	}

	client := NewClient(cfg)
	resolver, err := factory(cfg, client)
	if err != nil {
		return nil, AsError(err)
	}
	return resolver, nil
}
