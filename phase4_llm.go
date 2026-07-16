package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GeminiRequest maps to Google's Generative Language API payload
type GeminiRequest struct {
	Contents []Content `json:"contents"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

// ResolveWithGemini sends the isolated snippets to the Gemini API
func ResolveWithGemini(ctx context.Context, conflict ConflictContext, apiKey string) (string, error) {
	// Construct the highly specific prompt
	prompt := fmt.Sprintf(`You are a strict code merge resolver. Reconcile this Git conflict.
Language: %s
Node Type: %s
Context: %s

<<< LOCAL >>>
%s
=============
<<< REMOTE >>>
%s

Instructions: Merge the semantics logically. Return ONLY the resolved raw code block. No markdown formatting, no explanations.`,
		conflict.Language, conflict.NodeType, conflict.ContextLines, conflict.LocalSnippet, conflict.RemoteSnippet)

	// Build Gemini Payload
	reqBody := GeminiRequest{
		Contents: []Content{{
			Parts: []Part{{Text: prompt}},
		}},
	}
	payloadBytes, _ := json.Marshal(reqBody)

	// 1. Updated to use the stable 'v1' endpoint
	// 2. Updated target model to standard production 'gemini-2.5-flash'
	// (Note: If your key requires an older model, you can change this to "gemini-2.0-flash" or "gemini-1.5-flash")
	modelName := "gemini-3.5-flash"
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent?key=%s", modelName, apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Gemini API failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse the response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	// Extract the text part from Gemini's JSON structure
	return extractGeminiText(result)
}

func extractGeminiText(resp map[string]interface{}) (string, error) {
	// Safely navigating the Gemini response map: candidates[0].content.parts[0].text
	candidates, ok := resp["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return "", fmt.Errorf("no candidates returned")
	}

	candidate := candidates[0].(map[string]interface{})
	content, ok := candidate["content"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no content in candidate")
	}

	parts, ok := content["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		return "", fmt.Errorf("no parts in content")
	}

	part := parts[0].(map[string]interface{})
	text, ok := part["text"].(string)
	if !ok {
		return "", fmt.Errorf("text field missing")
	}

	// Clean up any residual markdown if the LLM ignores instructions
	text = strings.TrimPrefix(text, "```rust\n")
	text = strings.TrimPrefix(text, "```\n")
	text = strings.TrimSuffix(text, "\n```")
	return text, nil
}
