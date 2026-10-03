package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NewClient builds an HTTP client bounded by the configured request timeout.
// The context passed to each request additionally bounds every single attempt.
func NewClient(cfg Config) *http.Client {
	return &http.Client{Timeout: cfg.RequestTimeout}
}

// maxErrorBody limits how much of an error response body is echoed into
// error messages, keeping typed errors small and free of huge payloads.
const maxErrorBody = 512

// performRequest executes an HTTP request against a provider and returns the
// (bounded) response body. Non-2xx statuses become typed errors with the
// plan's retryability classification:
//
//   - retryable: 429, 500, 502, 503, 504
//   - non-retryable: everything else (authentication, bad request, ...)
//
// The request body is an optional JSON payload built by the caller.
func performRequest(ctx context.Context, client *http.Client, req *http.Request, provider string, maxResponseSize int, apiKey string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: DefaultRequestTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		// url.Error carries the full URL (including query credentials for
		// some providers); redactWithSecret strips any credentials, including
		// the configured API key.
		return nil, asErrorWithSecret(err, apiKey)
	}
	defer resp.Body.Close()

	body, err := readBodyLimited(resp.Body, maxResponseSize)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		retryable := resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusInternalServerError ||
			resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode == http.StatusGatewayTimeout
		snippet := string(body)
		if len(snippet) > maxErrorBody {
			snippet = snippet[:maxErrorBody]
		}
		// The provider may echo the rejected key back in its body: never leak it.
		message := redactWithSecret(fmt.Sprintf("%s API error (%d): %s", provider, resp.StatusCode, snippet), apiKey)
		return nil, &Error{Code: CodeHTTPError, Retryable: retryable, Message: message}
	}
	return body, nil
}

// readBodyLimited reads an HTTP body, rejecting responses that exceed the
// configured maximum size with a typed, non-retryable error.
func readBodyLimited(r io.Reader, maxBytes int) ([]byte, error) {
	limited := io.LimitReader(r, int64(maxBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, AsError(err)
	}
	if len(body) > maxBytes {
		return nil, NewError(CodeResponseTooLarge, false, "response exceeds maximum size of %d bytes", maxBytes)
	}
	return body, nil
}

// postJSON is the shared request builder for OpenAI-compatible providers.
func postJSON(ctx context.Context, url string, payload any) (*http.Request, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, NewError(CodeConfigInvalid, false, "failed to marshal provider request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		// Request construction errors can embed the URL (and credentials).
		return nil, NewError(CodeConfigInvalid, false, "%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// extractMessageContent pulls the assistant message content from an
// OpenAI-compatible chat completion response.
func extractMessageContent(body []byte, provider string) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", WrapError(CodeMalformedJSON, false, err, "failed to parse %s chat completion response", provider)
	}
	if len(parsed.Choices) == 0 {
		return "", NewError(CodeEmptyResponse, false, "%s returned no choices", provider)
	}
	content := parsed.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", NewError(CodeEmptyResponse, false, "%s returned an empty message", provider)
	}
	return content, nil
}

// BackoffDelay returns the bounded exponential backoff before retry attempt
// n (1-based): base * 2^(n-1), capped at MaxRetryBackoff.
func BackoffDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = DefaultRetryBackoff
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= MaxRetryBackoff {
			return MaxRetryBackoff
		}
	}
	if delay > MaxRetryBackoff {
		return MaxRetryBackoff
	}
	return delay
}
