package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	promptcontext "CommitIssues/internal/context"
	semantic "CommitIssues/internal/semantic"
)

func TestParseJSONResponse_Valid(t *testing.T) {
	res, err := parseJSONResponse(`{"explanation":"merged both","suggested_code":"function x(){}","confidence_score":85}`)
	if err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	if res.Explanation != "merged both" || res.Confidence != 85 {
		t.Errorf("parsed response = %+v", res)
	}
}

func TestParseJSONResponse_FencedJSON(t *testing.T) {
	res, err := parseJSONResponse("```json\n{\"explanation\":\"e\",\"suggested_code\":\"c\",\"confidence_score\":50}\n```")
	if err != nil {
		t.Fatalf("fenced JSON rejected: %v", err)
	}
	if res.Confidence != 50 {
		t.Errorf("confidence = %d", res.Confidence)
	}
	// Fence without a json language tag.
	if _, err := parseJSONResponse("```\n{\"explanation\":\"e\",\"suggested_code\":\"c\",\"confidence_score\":1}\n```"); err != nil {
		t.Fatalf("plain fenced JSON rejected: %v", err)
	}
}

func TestParseJSONResponse_UnknownFieldsIgnored(t *testing.T) {
	if _, err := parseJSONResponse(`{"explanation":"e","suggested_code":"c","confidence_score":60,"extra":true,"notes":"x"}`); err != nil {
		t.Fatalf("unknown fields must be ignored: %v", err)
	}
}

func TestParseJSONResponse_Invalid(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want ErrorCode
	}{
		{"malformed", `{"explanation": "e"`, CodeMalformedJSON},
		{"not json", `hello world`, CodeMalformedJSON},
		{"empty", ``, CodeEmptyResponse},
		{"missing explanation", `{"suggested_code":"c","confidence_score":10}`, CodeSchemaInvalid},
		{"empty explanation", `{"explanation":"   ","suggested_code":"c","confidence_score":10}`, CodeSchemaInvalid},
		{"missing suggested_code", `{"explanation":"e","confidence_score":10}`, CodeSchemaInvalid},
		{"empty suggested_code", `{"explanation":"e","suggested_code":"  ","confidence_score":10}`, CodeSchemaInvalid},
		{"missing confidence", `{"explanation":"e","suggested_code":"c"}`, CodeSchemaInvalid},
		{"confidence too high", `{"explanation":"e","suggested_code":"c","confidence_score":101}`, CodeSchemaInvalid},
		{"confidence negative", `{"explanation":"e","suggested_code":"c","confidence_score":-1}`, CodeSchemaInvalid},
		{"confidence wrong type", `{"explanation":"e","suggested_code":"c","confidence_score":"high"}`, CodeMalformedJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseJSONResponse(tc.raw)
			if err == nil {
				t.Fatalf("expected an error")
			}
			typed := AsError(err)
			if typed.Code != tc.want {
				t.Errorf("code = %q, want %q (%v)", typed.Code, tc.want, err)
			}
			if typed.Retryable {
				t.Errorf("schema/parse failures must never be retryable")
			}
		})
	}
}

func TestValidateResponse_Boundaries(t *testing.T) {
	for _, confidence := range []int{0, 1, 99, 100} {
		if err := ValidateResponse(&AIResolutionResponse{Explanation: "e", SuggestedCode: "c", Confidence: confidence}); err != nil {
			t.Errorf("confidence %d should be valid: %v", confidence, err)
		}
	}
	if err := ValidateResponse(nil); err == nil {
		t.Errorf("nil response must be invalid")
	}
}

func TestReadBodyLimited_RejectsOversized(t *testing.T) {
	if _, err := readBodyLimited(strings.NewReader(strings.Repeat("x", 100)), 50); err == nil {
		t.Fatalf("oversized body must be rejected")
	} else if AsError(err).Code != CodeResponseTooLarge {
		t.Errorf("code = %q, want %q", AsError(err).Code, CodeResponseTooLarge)
	}
	if _, err := readBodyLimited(strings.NewReader("small"), 50); err != nil {
		t.Errorf("small body rejected: %v", err)
	}
}

// ─── Retry policy ─────────────────────────────────────────────────────────────

type fakeResolver struct {
	calls     int
	responses []error // per-call error; nil means success
	delay     time.Duration
}

func diffItem() semantic.DiffItem {
	return semantic.DiffItem{Type: "COLLISION", Kind: "Function", Name: "f", Identity: "file|scope|Function|f|()"}
}

func promptCtx() promptcontext.PromptContextIR {
	return promptcontext.PromptContextIR{RepositorySummary: "repo", Context: "small prompt"}
}

func (f *fakeResolver) ResolveCollision(ctx context.Context, _ semantic.DiffItem, _ promptcontext.PromptContextIR) (*AIResolutionResponse, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if len(f.responses) == 0 {
		return nil, errors.New("no canned response")
	}
	resp := f.responses[0]
	if len(f.responses) > 1 {
		f.responses = f.responses[1:]
	}
	if resp != nil {
		return nil, resp
	}
	return &AIResolutionResponse{Explanation: "e", SuggestedCode: "c", Confidence: 80}, nil
}

func retryConfig() Config {
	cfg := Default("")
	cfg.RetryCount = 2
	cfg.RetryBackoff = time.Millisecond
	cfg.RequestTimeout = time.Second
	return cfg
}

func TestResolveCollisionWithRetry_RetriesTransientExactly(t *testing.T) {
	resolver := &fakeResolver{responses: []error{
		NewError(CodeHTTPError, true, "503"),
		NewError(CodeHTTPError, true, "503"),
		nil, // third attempt succeeds
	}}
	cfg := retryConfig()
	res, err := ResolveCollisionWithRetry(context.Background(), cfg, resolver, diffItem(), promptCtx())
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if res.Confidence != 80 {
		t.Errorf("unexpected response: %+v", res)
	}
	if resolver.calls != 3 {
		t.Errorf("calls = %d, want 3 (1 attempt + 2 retries)", resolver.calls)
	}
}

func TestResolveCollisionWithRetry_ExhaustsBoundedRetries(t *testing.T) {
	transient := NewError(CodeHTTPError, true, "503")
	resolver := &fakeResolver{responses: []error{transient}}
	cfg := retryConfig()
	_, err := ResolveCollisionWithRetry(context.Background(), cfg, resolver, diffItem(), promptCtx())
	if err == nil {
		t.Fatalf("expected failure after exhausting retries")
	}
	if resolver.calls != 3 {
		t.Errorf("calls = %d, want exactly RetryCount+1 = 3", resolver.calls)
	}
	if !AsError(err).Retryable {
		t.Errorf("final transient failure should stay retryable")
	}
}

func TestResolveCollisionWithRetry_DoesNotRetryPermanent(t *testing.T) {
	cases := map[string]error{
		"auth/http 401":    NewError(CodeHTTPError, false, "401 unauthorized"),
		"unsupported":      NewError(CodeProviderUnsupported, false, "nope"),
		"malformed json":   NewError(CodeMalformedJSON, false, "bad"),
		"schema invalid":   NewError(CodeSchemaInvalid, false, "bad"),
		"prompt too large": NewError(CodePromptTooLarge, false, "huge"),
	}
	for name, canned := range cases {
		t.Run(name, func(t *testing.T) {
			resolver := &fakeResolver{responses: []error{canned}}
			_, err := ResolveCollisionWithRetry(context.Background(), retryConfig(), resolver, diffItem(), promptCtx())
			if err == nil {
				t.Fatalf("expected error")
			}
			if resolver.calls != 1 {
				t.Errorf("permanent failure must not be retried: calls = %d", resolver.calls)
			}
		})
	}
}

func TestResolveCollisionWithRetry_CancelNotRetried(t *testing.T) {
	resolver := &fakeResolver{responses: []error{NewError(CodeHTTPError, true, "503")}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ResolveCollisionWithRetry(ctx, retryConfig(), resolver, diffItem(), promptCtx())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if resolver.calls != 0 {
		t.Errorf("cancelled request must not call the provider, calls = %d", resolver.calls)
	}
}

func TestResolveCollisionWithRetry_DeadlineNotRetried(t *testing.T) {
	resolver := &fakeResolver{responses: []error{NewError(CodeHTTPError, true, "503")}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-ctx.Done()
	_, err := ResolveCollisionWithRetry(ctx, retryConfig(), resolver, diffItem(), promptCtx())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if resolver.calls != 0 {
		t.Errorf("expired request must not call the provider, calls = %d", resolver.calls)
	}
}

func TestResolveCollisionWithRetry_PerAttemptTimeout(t *testing.T) {
	resolver := &fakeResolver{responses: []error{nil}, delay: 200 * time.Millisecond}
	cfg := retryConfig()
	cfg.RequestTimeout = 10 * time.Millisecond
	_, err := ResolveCollisionWithRetry(context.Background(), cfg, resolver, diffItem(), promptCtx())
	if err == nil {
		t.Fatalf("expected a per-attempt timeout error")
	}
	typed := AsError(err)
	if typed.Code != CodeTimeout {
		t.Errorf("code = %q, want %q", typed.Code, CodeTimeout)
	}
	if typed.Retryable {
		t.Errorf("timeouts must not be retried")
	}
}

func TestResolveCollisionWithRetry_PromptTooLargeFailsFast(t *testing.T) {
	resolver := &fakeResolver{responses: []error{nil}}
	cfg := retryConfig()
	cfg.MaxPromptSize = 4
	large := promptCtx()
	large.Context = "this prompt is far too large"
	_, err := ResolveCollisionWithRetry(context.Background(), cfg, resolver, diffItem(), large)
	if err == nil {
		t.Fatalf("expected prompt-too-large error")
	}
	if AsError(err).Code != CodePromptTooLarge {
		t.Errorf("code = %q", AsError(err).Code)
	}
	if resolver.calls != 0 {
		t.Errorf("oversized prompt must not reach the provider")
	}
}

// ─── Provider integration (fake HTTP servers) ─────────────────────────────────

func TestProvider_RetryableStatusRetriedThroughHTTP(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporarily down"))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"explanation\":\"e\",\"suggested_code\":\"c\",\"confidence_score\":77}"}}]}`))
	}))
	defer srv.Close()

	cfg := Default("")
	cfg.BaseURL = srv.URL
	cfg.RetryBackoff = time.Millisecond
	resolver, err := GetResolver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveCollisionWithRetry(context.Background(), cfg, resolver, diffItem(), promptCtx())
	if err != nil {
		t.Fatalf("expected eventual success: %v", err)
	}
	if res.Confidence != 77 || attempts != 3 {
		t.Errorf("confidence=%d attempts=%d, want 77 and 3", res.Confidence, attempts)
	}
}

func TestProvider_NonRetryableStatusFailsImmediately(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key"))
	}))
	defer srv.Close()

	cfg := Default("")
	cfg.BaseURL = srv.URL
	cfg.RetryBackoff = time.Millisecond
	resolver, _ := GetResolver(cfg)
	_, err := ResolveCollisionWithRetry(context.Background(), cfg, resolver, diffItem(), promptCtx())
	if err == nil {
		t.Fatal("expected failure")
	}
	if AsError(err).Retryable {
		t.Errorf("401 must not be retryable")
	}
	if attempts != 1 {
		t.Errorf("401 must be attempted once, got %d", attempts)
	}
}

func TestProvider_OversizedResponseRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		big := strings.Repeat("x", 4096)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + big + `"}}]}`))
	}))
	defer srv.Close()

	cfg := Default("")
	cfg.BaseURL = srv.URL
	cfg.MaxResponseSize = 256
	resolver, _ := GetResolver(cfg)
	_, err := ResolveCollisionWithRetry(context.Background(), cfg, resolver, diffItem(), promptCtx())
	if err == nil {
		t.Fatal("expected oversized response rejection")
	}
	if AsError(err).Code != CodeResponseTooLarge {
		t.Errorf("code = %q, want %q", AsError(err).Code, CodeResponseTooLarge)
	}
}
