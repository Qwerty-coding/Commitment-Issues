
# Phase 2 Final Plan

## Objective

Make AI suggestions reliable, configurable, cancellable, validated, and isolated. Repair the History API separately. Do not apply or modify repository files.

## Priority Order

1. Provider configuration
2. Request context and timeouts
3. Retry logic
4. Response validation
5. Concurrency control
6. Run-scoped suggestions
7. History API
8. Minimal frontend updates

## Phase 2A: Reliable AI Suggestions

### 1. Centralize configuration

Create one shared AI configuration used by CLI, backend API, and suggestion generation.

Configuration fields:

- Provider
- Model
- Base URL
- API key
- Request timeout
- Retry count
- Confidence threshold
- Maximum prompt size
- Maximum response size

Configuration precedence:

1. CLI flags
2. Environment variables
3. Provider defaults

Recommended environment variables:

```text
AI_PROVIDER=ollama
AI_MODEL=qwen2.5-coder:7b
AI_BASE_URL=http://localhost:11434/v1
AI_API_KEY=
AI_TIMEOUT=60s
AI_RETRIES=2
AI_CONFIDENCE_THRESHOLD=70
```

Rules:

- Ollama is the default provider only when no provider is configured.
- The default model must be documented and configurable.
- Do not silently switch providers or models.
- `analyze` remains AI-free.
- `serve` remains AI-free.
- Only `resolve` and Suggestions invoke AI.
- The selected provider and model must be visible in logs and response metadata.

### 2. Provider health checks

Implement provider checks before generation:

- Ollama is reachable.
- Requested Ollama model exists.
- Gemini/Groq API keys are present.
- Base URLs are valid.
- Unsupported providers fail clearly.

Errors must be typed and actionable. API keys must never appear in logs or responses.

### 3. Context, timeout, and cancellation

Propagate `context.Context` through:

- HTTP handler
- Suggestion generation
- Engine
- Resolver
- HTTP request

Requirements:

- Use configured per-request timeouts.
- Cancellation returns `context.Canceled`.
- Timeout returns `context.DeadlineExceeded`.
- Cancelled requests must not be retried.
- No goroutine or network request may outlive its parent request.

### 4. Retry policy

Retry only transient failures:

- Connection reset
- HTTP `429`
- HTTP `500`, `502`, `503`, `504`

Do not retry:

- Authentication failures
- Invalid configuration
- Unsupported provider
- Malformed JSON
- Schema validation failures
- Cancellation
- Deadline expiration

Use bounded exponential backoff and test the exact retry count.

### 5. Response schema

The canonical provider response must be:

```json
{
  "explanation": "string",
  "suggested_code": "string",
  "confidence_score": 85
}
```

Validation rules:

- `explanation` is required and non-empty.
- `suggested_code` is required and non-empty for successful resolutions.
- `confidence_score` is required and must be between `0` and `100`.
- Unknown fields may be preserved or ignored.
- Malformed or invalid responses are typed errors.
- Responses exceeding the configured size limit are rejected.

If camelCase is desired for frontend responses, translate it explicitly at the API boundary. Do not mix schemas internally.

### 6. Bounded concurrency

For multiple files and collisions:

- Limit concurrent AI requests.
- Do not hold global locks during network calls.
- Preserve deterministic file and collision ordering.
- Preserve successful suggestions if another collision fails.
- Return structured partial-failure information.
- Avoid duplicate requests for the same run/file/collision identity.

### 7. Run-scoped suggestions

Store suggestions in `runstate.Run`, keyed by:

```text
run ID + repository root + file + collision identity
```

Each suggestion should include:

- Stable suggestion ID
- Repository
- File
- Collision identity
- Provider
- Model
- Confidence
- Status
- Error code, if failed
- Start timestamp
- Completion timestamp

Preserve the existing `SuggestionItem` fields and add metadata only additively.

## Phase 2B: Run History and Audit API

Do not restore the obsolete `/api/prompt/statistics` endpoint unless real statistics are implemented.

Add a history read model exposing:

- Run ID
- Repository
- Start time
- Completion time
- Files analyzed
- Collision count
- Provider
- Model
- Successful suggestions
- Failed suggestions
- Timeout/cancellation status
- Error summaries

Use bounded in-memory history for Phase 2. Database persistence is out of scope.

History must be deterministic and isolated between runs and repositories.

## Minimal Frontend Work

Frontend changes are limited to:

- Consume the new suggestion API metadata.
- Replace the broken History endpoint.
- Display provider/model/status information.
- Support loading, empty, timeout, unavailable-provider, and partial-failure states.
- Add retry where the API reports a retryable failure.

Frontend test coverage is recommended, but it should not block backend Phase 2 completion. However, basic manual API/UI verification is required.

## Tests

### Backend tests

Use fake providers and fake HTTP servers to test:

- CLI configuration overrides environment variables.
- Environment variables override defaults.
- Ollama unavailable.
- Ollama model missing.
- Gemini/Groq API key missing.
- Request cancellation.
- Request timeout.
- Retryable status codes.
- Non-retryable status codes.
- Retry count and backoff bounds.
- Malformed JSON.
- Fenced JSON.
- Missing required fields.
- Invalid confidence values.
- Oversized responses.
- Multiple collisions.
- Bounded concurrency.
- Deterministic ordering.
- Partial failures.
- Duplicate request prevention.
- Run isolation.
- Repository isolation.
- History ordering and isolation.
- No secret leakage in errors or logs.

### Frontend verification

Required manual checks:

- Suggestions load successfully.
- Ollama unavailable state is understandable.
- Timeout state is shown.
- Partial failures retain successful suggestions.
- History loads from the new endpoint.
- History empty state works.
- Retry works for retryable errors.
- Existing dashboard and conflict pages remain compatible.

## Verification Commands

```text
cd backend
go test ./...
go test -race ./...
go vet ./...

cd ../frontend
npm run lint
npm run build
```

## Explicitly Out Of Scope

- Applying suggestions to files.
- Patch preview.
- Approval workflow.
- Rollback.
- Automatic commits.
- Autonomous conflict resolution.
- Database persistence.
- Distributed jobs.
- Broad frontend redesign.
- Graph feature work.
- General API security and graceful server shutdown.

This version is focused enough to give directly to the developer or another AI agent.