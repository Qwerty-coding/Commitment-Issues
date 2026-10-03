package ai

import (
	"context"
	"net/http"

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
	// MaxResponseSize bounds the accepted HTTP response body.
	MaxResponseSize int
}

func newGroqResolver(cfg Config, client *http.Client) (Resolver, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultGroqBaseURL
	}
	if _, err := ValidateBaseURL(baseURL, cfg.Provider); err != nil {
		return nil, err
	}
	if client == nil {
		client = NewClient(cfg)
	}
	return &GroqResolver{
		BaseURL:         baseURL,
		Model:           cfg.Model,
		APIKey:          cfg.APIKey,
		Client:          client,
		MaxResponseSize: cfg.MaxResponseSize,
	}, nil
}

func (g *GroqResolver) ResolveCollision(ctx context.Context, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	// Groq uses the exact same payload schema as OpenAI.
	return resolveOpenAICompatible(ctx, g.Client, g.BaseURL, g.Model, g.APIKey, g.APIKey, collision, promptCtx, "groq", g.MaxResponseSize)
}
