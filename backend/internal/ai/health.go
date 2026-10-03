package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// CheckProvider runs actionable pre-flight health checks before any
// generation:
//
//   - the provider is supported and the configuration is valid,
//   - for Ollama: the server is reachable and the requested model exists,
//   - for Gemini/Groq: the API key is present,
//   - base URLs are valid,
//   - unsupported providers fail clearly.
//
// API keys never appear in returned errors. Every failure is typed.
func CheckProvider(ctx context.Context, cfg Config, client *http.Client) error {
	if err := ctx.Err(); err != nil {
		return AsError(err)
	}
	if err := cfg.Validate(); err != nil {
		return NewError(CodeConfigInvalid, false, "%v", err)
	}

	switch strings.ToLower(cfg.Provider) {
	case "ollama":
		return checkOllama(ctx, cfg, client)
	case "gemini", "groq":
		if strings.TrimSpace(cfg.APIKey) == "" {
			return NewError(CodeAPIKeyMissing, false,
				"%s requires an API key; set AI_API_KEY or --key", cfg.Provider)
		}
		return nil
	default:
		return NewError(CodeProviderUnsupported, false,
			"unsupported AI provider %q; available: %s", cfg.Provider, strings.Join(ListProviders(), ", "))
	}
}

// ollamaServerRoot derives the native Ollama API root from the configured
// (possibly OpenAI-compatible) base URL: http://host:11434/v1 -> http://host:11434.
func ollamaServerRoot(baseURL string) string {
	root := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if root == "" {
		root = DefaultOllamaBaseURL
	}
	if strings.HasSuffix(root, "/v1") {
		root = strings.TrimSuffix(root, "/v1")
	}
	return root
}

// normalizeOllamaModel treats an untagged model as the implicit ":latest" tag
// so that "qwen2" and "qwen2:latest" compare equal.
func normalizeOllamaModel(model string) string {
	model = strings.TrimSpace(model)
	if !strings.Contains(model, ":") {
		return model + ":latest"
	}
	return model
}

func checkOllama(ctx context.Context, cfg Config, client *http.Client) error {
	root := ollamaServerRoot(cfg.BaseURL)
	if client == nil {
		client = NewClient(cfg)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"/api/tags", nil)
	if err != nil {
		return NewError(CodeBaseURLInvalid, false, "%v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Unreachable endpoint: transient, actionable, typed.
		return WrapError(CodeProviderUnavailable, true, err,
			"Ollama is not reachable at %s (is it running?)", root)
	}
	defer resp.Body.Close()

	body, err := readBodyLimited(resp.Body, cfg.MaxResponseSize)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return NewError(CodeProviderUnavailable, resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests,
			"Ollama health check failed at %s with status %d", root, resp.StatusCode)
	}

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		return WrapError(CodeMalformedJSON, false, err, "failed to parse Ollama model list")
	}

	wanted := normalizeOllamaModel(cfg.Model)
	for _, m := range tags.Models {
		if normalizeOllamaModel(m.Name) == wanted {
			return nil
		}
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return NewError(CodeModelMissing, false,
		"model %q is not available on Ollama; installed models: %s", cfg.Model, strings.Join(names, ", "))
}
