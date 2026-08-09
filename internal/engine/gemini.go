// gemini implements the Gemini-backed provider for sending conflict payloads to the AI model.
package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/toon-format/toon-go"
	"google.golang.org/api/option"
)

// GeminiProvider is the current provider implementation.
type GeminiProvider struct{}

// NewGeminiProvider creates a Gemini-backed provider.
func NewGeminiProvider() (Provider, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}
	return &GeminiProvider{}, nil
}

// Resolve sends the conflict payload to Gemini and returns the merged code.
func (g *GeminiProvider) Resolve(payload ConflictPayload, fullLocalCode string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	toonBytes, err := toon.Marshal(payload, toon.WithLengthMarkers(true))
	if err != nil {
		return "", fmt.Errorf("failed to marshal TOON payload: %w", err)
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-3.5-flash")
	model.SetTemperature(0.1)

	prompt := fmt.Sprintf(`You are an expert compiler engineer resolving Git merge conflicts.
Below is an AST operational diff in TOON format.

=== AST DIFF (TOON) ===
%s

=== FULL LOCAL CONTEXT ===
%s

Return only the valid merged source code for the file.`, string(toonBytes), fullLocalCode)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("gemini API call failed: %w", err)
	}
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != 0 {
		return "", fmt.Errorf("gemini blocked the prompt: %v", resp.PromptFeedback.BlockReason)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0] == nil || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("empty response received from Gemini")
	}

	var builder strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		switch v := part.(type) {
		case genai.Text:
			builder.WriteString(string(v))
		default:
			builder.WriteString(fmt.Sprintf("%v", v))
		}
	}

	resolvedCode := strings.TrimSpace(builder.String())
	resolvedCode = strings.ReplaceAll(resolvedCode, "```toon", "")
	resolvedCode = strings.ReplaceAll(resolvedCode, "```", "")
	resolvedCode = strings.TrimSpace(resolvedCode)
	if resolvedCode == "" {
		return "", fmt.Errorf("gemini returned an empty candidate body")
	}

	return resolvedCode, nil
}
