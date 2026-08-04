package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
	"gopkg.in/yaml.v3"
)

// ResolveConflictWithGemini serializes payload to YAML and queries Gemini.
func ResolveConflictWithGemini(payload ConflictPayload, fullLocalCode string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	yamlBytes, err := yaml.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML payload: %w", err)
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
Below is an AST operational diff encoded in YAML along with the parent function scope context.

=== AST DIFF (YAML) ===
%s

=== FULL LOCAL CONTEXT ===
%s

Instructions:
1. Reconcile the local and remote operations cleanly.
2. Return ONLY the valid merged source code for the file.
3. Do NOT wrap output in markdown code blocks, do not add explanations.`, string(yamlBytes), fullLocalCode)

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
	resolvedCode = strings.ReplaceAll(resolvedCode, "```yaml", "")
	resolvedCode = strings.ReplaceAll(resolvedCode, "```", "")
	resolvedCode = strings.TrimSpace(resolvedCode)

	if resolvedCode == "" {
		return "", fmt.Errorf("gemini returned an empty candidate body")
	}

	return resolvedCode, nil
}
