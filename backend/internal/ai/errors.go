package ai

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrorCode is a stable, machine-readable identifier for an AI subsystem
// failure. Codes are part of the API contract: the frontend maps them to
// user-facing states (unavailable provider, timeout, retryable failure...).
type ErrorCode string

const (
	// CodeProviderUnsupported indicates the requested provider is not registered.
	CodeProviderUnsupported ErrorCode = "PROVIDER_UNSUPPORTED"
	// CodeConfigInvalid indicates the AI configuration failed validation.
	CodeConfigInvalid ErrorCode = "AI_CONFIG_INVALID"
	// CodeAPIKeyMissing indicates the provider requires an API key that is absent.
	CodeAPIKeyMissing ErrorCode = "API_KEY_MISSING"
	// CodeBaseURLInvalid indicates the configured base URL cannot be parsed.
	CodeBaseURLInvalid ErrorCode = "BASE_URL_INVALID"
	// CodeProviderUnavailable indicates the provider endpoint is unreachable.
	CodeProviderUnavailable ErrorCode = "PROVIDER_UNAVAILABLE"
	// CodeModelMissing indicates the requested model does not exist on the provider.
	CodeModelMissing ErrorCode = "MODEL_MISSING"
	// CodeHTTPError indicates a non-retryable HTTP error from the provider.
	CodeHTTPError ErrorCode = "HTTP_ERROR"
	// CodeMalformedJSON indicates the provider returned unparsable JSON.
	CodeMalformedJSON ErrorCode = "MALFORMED_JSON"
	// CodeSchemaInvalid indicates the parsed response violated the required schema.
	CodeSchemaInvalid ErrorCode = "SCHEMA_INVALID"
	// CodeResponseTooLarge indicates the response exceeded the configured size limit.
	CodeResponseTooLarge ErrorCode = "RESPONSE_TOO_LARGE"
	// CodeEmptyResponse indicates the provider returned no usable content.
	CodeEmptyResponse ErrorCode = "EMPTY_RESPONSE"
	// CodePromptTooLarge indicates the prompt exceeded the configured size limit.
	CodePromptTooLarge ErrorCode = "PROMPT_TOO_LARGE"
	// CodeCancelled indicates the parent context was cancelled.
	CodeCancelled ErrorCode = "CANCELLED"
	// CodeTimeout indicates a request deadline was exceeded.
	CodeTimeout ErrorCode = "TIMEOUT"
)

// Error is a typed AI subsystem error. Retryable marks failures the retry
// policy may re-attempt (transient network/HTTP conditions only).
type Error struct {
	Code      ErrorCode
	Message   string
	Retryable bool
	Cause     error
}

func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

// Unwrap exposes the underlying cause for errors.Is/As chains.
func (e *Error) Unwrap() error { return e.Cause }

// NewError builds a typed error. It redacts secrets from the message.
func NewError(code ErrorCode, retryable bool, format string, args ...any) *Error {
	return &Error{Code: code, Retryable: retryable, Message: redact(fmt.Sprintf(format, args...))}
}

// WrapError builds a typed error wrapping cause, redacting secrets from it.
func WrapError(code ErrorCode, retryable bool, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Retryable: retryable, Message: redact(fmt.Sprintf(format, args...)), Cause: cause}
}

// AsError converts an arbitrary error into a typed *Error, preserving typed
// errors as-is and classifying context cancellation/timeout.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeTimeout, Message: redact(err.Error())}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: CodeCancelled, Message: redact(err.Error())}
	}
	// url.Error from an unreachable endpoint is a transient transport failure.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return &Error{
			Code:      CodeProviderUnavailable,
			Retryable: true,
			Message:   redact(err.Error()),
			Cause:     err,
		}
	}
	return &Error{Message: redact(err.Error()), Cause: err}
}

// redactWithSecret strips both generic credential patterns and any literal
// occurrence of the configured secret (providers sometimes echo a rejected
// key back in an error body).
func redactWithSecret(s, secret string) string {
	out := redact(s)
	if secret != "" {
		out = strings.ReplaceAll(out, secret, "[REDACTED]")
	}
	return out
}

// asErrorWithSecret classifies an arbitrary error while guaranteeing the
// configured secret never survives in the message.
func asErrorWithSecret(err error, secret string) *Error {
	typed := AsError(err)
	if typed == nil {
		return nil
	}
	typed.Message = redactWithSecret(typed.Message, secret)
	return typed
}

// redact removes API keys and credentials from an error string so that no
// secret can ever reach logs, CLI output or API responses.
func redact(s string) string {
	out := s
	// Gemini-style key query parameters: ?key=SECRET / &key=SECRET
	out = redactQueryParam(out, "key")
	out = redactQueryParam(out, "api_key")
	out = redactQueryParam(out, "apikey")
	out = redactQueryParam(out, "access_token")
	// Authorization headers: "Authorization: Bearer SECRET" / "Bearer SECRET"
	out = redactBearer(out)
	return out
}

// redactQueryParam replaces the value of a query-style parameter in a single
// forward pass (no re-scanning of the replacement, so it always terminates).
func redactQueryParam(s, param string) string {
	separators := []string{"?" + param + "=", "&" + param + "=", " " + param + "="}

	var b strings.Builder
	i := 0
	for i < len(s) {
		best := -1
		bestLen := 0
		for _, sep := range separators {
			if idx := strings.Index(s[i:], sep); idx >= 0 {
				abs := i + idx
				if best == -1 || abs < best {
					best = abs
					bestLen = len(sep)
				}
			}
		}
		if best == -1 {
			b.WriteString(s[i:])
			break
		}

		valueStart := best + bestLen
		valueEnd := valueStart
		for valueEnd < len(s) {
			c := s[valueEnd]
			if c == '&' || c == '"' || c == '\'' || c == ' ' || c == ')' {
				break
			}
			valueEnd++
		}

		b.WriteString(s[i:valueStart])
		b.WriteString("[REDACTED]")
		i = valueEnd
	}
	return b.String()
}

func redactBearer(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		lower := strings.ToLower(line)
		if idx := strings.Index(lower, "bearer "); idx >= 0 {
			rest := line[idx+len("bearer "):]
			trimmed := strings.TrimLeft(rest, " ")
			if trimmed != "" {
				b.WriteString(line[:idx+len("bearer ")] + " [REDACTED]")
				continue
			}
		}
		b.WriteString(line)
	}
	return b.String()
}
