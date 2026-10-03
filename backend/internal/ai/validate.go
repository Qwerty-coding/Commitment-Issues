package ai

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

// aiResolutionWire mirrors AIResolutionResponse with pointer fields so that a
// *missing* field is distinguishable from an explicit empty/zero value during
// schema validation. The canonical provider response is:
//
//	{"explanation": string, "suggested_code": string, "confidence_score": int}
//
// Unknown fields are ignored. camelCase API translation happens explicitly at
// the API boundary; the internal schema is snake_case only.
type aiResolutionWire struct {
	Explanation     *string `json:"explanation"`
	SuggestedCode   *string `json:"suggested_code"`
	ConfidenceScore *int    `json:"confidence_score"`
}

// ValidateResponse enforces the canonical response schema:
//   - explanation: required, non-empty
//   - suggested_code: required, non-empty (for successful resolutions)
//   - confidence_score: required, between 0 and 100
//
// Invalid responses are typed, non-retryable errors: no amount of retrying
// fixes a schema violation.
func ValidateResponse(res *AIResolutionResponse) error {
	if res == nil {
		return NewError(CodeSchemaInvalid, false, "provider returned no resolution")
	}
	if strings.TrimSpace(res.Explanation) == "" {
		return NewError(CodeSchemaInvalid, false, "response schema invalid: explanation is required and must be non-empty")
	}
	if strings.TrimSpace(res.SuggestedCode) == "" {
		return NewError(CodeSchemaInvalid, false, "response schema invalid: suggested_code is required and must be non-empty")
	}
	if res.Confidence < 0 || res.Confidence > 100 {
		return NewError(CodeSchemaInvalid, false, "response schema invalid: confidence_score must be between 0 and 100, got %d", res.Confidence)
	}
	return nil
}

// parseJSONResponse parses raw provider text into a validated
// AIResolutionResponse. It tolerates fenced ```json blocks, which small local
// models frequently emit despite the response_format instruction.
func parseJSONResponse(rawText string) (*AIResolutionResponse, error) {
	rawText = strings.TrimSpace(rawText)
	rawText = strings.TrimPrefix(rawText, "```json")
	rawText = strings.TrimPrefix(rawText, "```")
	rawText = strings.TrimSuffix(rawText, "```")
	rawText = strings.TrimSpace(rawText)
	if rawText == "" {
		return nil, NewError(CodeEmptyResponse, false, "provider returned empty content")
	}

	var wire aiResolutionWire
	if err := json.Unmarshal([]byte(rawText), &wire); err != nil {
		return nil, WrapError(CodeMalformedJSON, false, err, "failed to parse AI resolution JSON")
	}
	if wire.Explanation == nil {
		return nil, NewError(CodeSchemaInvalid, false, "response schema invalid: explanation is required")
	}
	if wire.SuggestedCode == nil {
		return nil, NewError(CodeSchemaInvalid, false, "response schema invalid: suggested_code is required")
	}
	if wire.ConfidenceScore == nil {
		return nil, NewError(CodeSchemaInvalid, false, "response schema invalid: confidence_score is required")
	}
	res := &AIResolutionResponse{
		Explanation:   *wire.Explanation,
		SuggestedCode: *wire.SuggestedCode,
		Confidence:    *wire.ConfidenceScore,
	}
	if err := ValidateResponse(res); err != nil {
		return nil, err
	}
	return res, nil
}

// ResolveCollisionWithRetry executes one collision resolution with the
// configured retry policy: bounded attempts, bounded exponential backoff and
// a per-attempt request timeout. Cancellation and deadline expiration are
// never retried; only transient transport/HTTP failures are.
func ResolveCollisionWithRetry(ctx context.Context, cfg Config, resolver Resolver, collision semantic.DiffItem, promptCtx promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	// Cancellation and deadline expiry return the raw context errors so that
	// callers keep the Phase 1 contract (errors.Is(err, context.Canceled)).
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Prompt size is enforced before any network traffic so oversized
	// contexts fail fast with a typed, actionable error.
	if len(promptCtx.Context) > cfg.MaxPromptSize {
		return nil, NewError(CodePromptTooLarge, false,
			"prompt context of %d bytes exceeds the configured maximum of %d bytes (AI_MAX_PROMPT_BYTES)",
			len(promptCtx.Context), cfg.MaxPromptSize)
	}

	var lastErr error
	for attempt := 0; attempt <= cfg.RetryCount; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt > 0 {
			// Cancellation during the backoff must abort the retry loop.
			timer := time.NewTimer(BackoffDelay(cfg.RetryBackoff, attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
		res, err := resolver.ResolveCollision(attemptCtx, collision, promptCtx)
		cancel()

		if err == nil {
			if validateErr := ValidateResponse(res); validateErr != nil {
				return nil, validateErr
			}
			return res, nil
		}

		// Parent cancellation/deadline always wins over a provider error and
		// is returned raw (never retried).
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		lastErr = AsError(err)
		// Never retry: cancellation, timeout, or non-transient failures.
		typed, ok := lastErr.(*Error)
		if !ok || !typed.Retryable {
			return nil, lastErr
		}
	}
	return nil, lastErr
}
