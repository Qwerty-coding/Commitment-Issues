# Module: AI

## Purpose

The AI package is the provider abstraction layer: a self-registering
resolver registry, a single shared configuration with strict precedence
(flags > env > provider defaults), typed/redacted error handling with a
retry policy, pre-flight health checks, strict response-schema
validation, and three concrete providers (Ollama, Groq — OpenAI-compatible;
Gemini — Google's Generative Language API). Everything outside this
package talks to AI through `Resolver` + `ResolveCollisionWithRetry`.

## Files

### `backend/internal/ai/ai.go`

#### Primary Role
Core contracts: response type, `Resolver` interface, factory type,
system prompt, and the dynamic provider registry.

#### Key Structures
- `AIResolutionResponse{Explanation, SuggestedCode, Confidence
  (json:"confidence_score")}` — the uniform response from all providers.
- `Resolver` interface: `ResolveCollision(ctx, collision
  semantic.DiffItem, promptCtx promptcontext.PromptContextIR)
  (*AIResolutionResponse, error)` — the context is always the caller's;
  no provider may use `context.Background()`.
- `FactoryFunc func(cfg Config, client *http.Client) (Resolver, error)`.
- `defaultSystemPrompt` — shared across providers: structural-level
  conflict resolution strategy (merge if compatible, state trade-offs,
  preserve both intents, rate confidence 0-100) and the exact JSON output
  schema.
- Registry: `registryMutex sync.RWMutex` + `providers
  map[string]FactoryFunc`; `Register(name, factory)` (lowercased keys),
  `ListProviders()` (sorted — insertion-order-independent, via an
  explicit in-place sort), `GetResolver(cfg)` (unknown provider → typed
  `CodeProviderUnsupported` listing available names; validates config;
  builds a timeout-bounded client; wraps factory errors with `AsError`).

#### Algorithms & Logic
Registry is **write-once init config**: providers self-register in
`init()` in `ollama.go`/`groq.go`/`gemini.go` — the only package-level
state allowed by the architecture (never run data; suggestion/analysis
state lives in `runstate`).

### `backend/internal/ai/config.go`

#### Primary Role
The single shared `Config` type, env-var parsing, provider defaults,
deterministic validation, and URL/key sanitation.

#### Key Structures
- Env constants: `AI_PROVIDER`, `AI_MODEL`, `AI_BASE_URL`, `AI_API_KEY`,
  `AI_TIMEOUT`, `AI_RETRIES`, `AI_CONFIDENCE_THRESHOLD`,
  `AI_MAX_PROMPT_BYTES`, `AI_MAX_RESPONSE_BYTES`, `AI_RETRY_BACKOFF`,
  `AI_README_CONTEXT`, `AI_README_MAX_BYTES`, legacy
  `OLLAMA_BASE_URL`.
- Documented defaults: provider `ollama`, Ollama model
  `qwen2.5-coder:7b` @ `http://localhost:11434/v1`, Gemini model
  `gemini-3.5-flash` @ `generativelanguage.googleapis.com/v1beta`, Groq
  model `llama-3.3-70b-versatile` @ `api.groq.com/openai/v1`, timeout
  60s, retries 2, threshold 70, prompt cap 512 KB, response cap 1 MB,
  backoff 500 ms → cap 4 s.
- `Config{Provider, Model, BaseURL, APIKey (json:"-"), RequestTimeout,
  RetryCount, RetryBackoff, ConfidenceThreshold, MaxPromptSize,
  MaxResponseSize}` + alias `AIConfig`.
- `Default(provider)` — per-provider documented defaults (unknown →
  Ollama).
- `ConfigFromEnv()` — defaults selected from `AI_PROVIDER`, then env
  overrides applied; never silently switches provider/model.
- `(c Config) ApplyProviderDefaults(provider)` — re-applies model/base-URL
  defaults for a *flag-chosen* provider **only when the corresponding env
  var is unset** (`baseURLSetFromEnv` accounts for the legacy
  `OLLAMA_BASE_URL`), preserving precedence flags > env > defaults.
- `ConfigError{Field, Value, Message}` (code `AI_CONFIG_INVALID`),
  `Validate()` (fixed order: provider required → registered → model
  required → timeout > 0 → retries ≥ 0 → backoff ≥ 0 → threshold 0–100 →
  prompt/response sizes > 0 → base URL), `ValidateBaseURL` (empty allowed
  — provider default at construction; must be `http(s)://` + parseable
  host), `Sanitized()` (API key stripped for logs/API metadata).

#### Algorithms & Logic
Precedence assembly happens at the *caller* (e.g. `cmd/resolve.go`
`buildAIConfigFromFlags`): `ConfigFromEnv` → `ApplyProviderDefaults` on
provider switch → explicit flags last. This package only supplies the
mechanics, so no layer can accidentally reorder precedence.

### `backend/internal/ai/errors.go`

#### Primary Role
The typed error contract: stable codes, retryability classification, and
two-layer secret redaction.

#### Key Structures
- `ErrorCode` set: `PROVIDER_UNSUPPORTED`, `AI_CONFIG_INVALID`,
  `API_KEY_MISSING`, `BASE_URL_INVALID`, `PROVIDER_UNAVAILABLE`,
  `MODEL_MISSING`, `HTTP_ERROR`, `MALFORMED_JSON`, `SCHEMA_INVALID`,
  `RESPONSE_TOO_LARGE`, `EMPTY_RESPONSE`, `PROMPT_TOO_LARGE`,
  `CANCELLED`, `TIMEOUT` — part of the API contract (frontend maps them
  to UX states).
- `Error{Code, Message, Retryable, Cause}` with `Unwrap`;
  constructors `NewError` / `WrapError` (both redact), classifier
  `AsError` (preserves typed errors; `context.DeadlineExceeded` →
  `TIMEOUT`; `context.Canceled` → `CANCELLED`; `*url.Error` →
  `PROVIDER_UNAVAILABLE` **retryable**; otherwise generic redacted).
- Redaction: `redact` (query params `key`/`api_key`/`apikey`/
  `access_token` and `Bearer` tokens, line-aware), `redactWithSecret`
  (also strips the literal configured API key — providers echo rejected
  keys), `asErrorWithSecret` (classify then scrub).

#### Algorithms & Logic
`redactQueryParam` does a single forward pass choosing the earliest
separator match and consuming until `&`/quote/space/`)` — guaranteed
termination and no re-scan of replacements (no double-redaction bugs).

### `backend/internal/ai/http.go`

#### Primary Role
Shared HTTP plumbing: client construction, bounded request execution with
retryability classification, and OpenAI-compatible response extraction.

#### Key Structures
- `NewClient(cfg)` — `http.Client{Timeout: cfg.RequestTimeout}`.
- `maxErrorBody = 512` — error snippets are truncated.
- `performRequest(ctx, client, req, provider, maxResponseSize, apiKey)`
  — non-2xx → typed `CodeHTTPError` with **retryable iff status ∈ {429,
  500, 502, 503, 504}**, body snippet redacted with the secret;
  transport errors → `asErrorWithSecret`.
- `readBodyLimited` — `io.LimitReader(maxBytes+1)`; overflow → typed
  non-retryable `RESPONSE_TOO_LARGE`.
- `postJSON` (shared OpenAI-style builder; marshal/build failures →
  `AI_CONFIG_INVALID` with URL credentials scrubbed),
  `extractMessageContent` (`choices[0].message.content`; empty →
  `EMPTY_RESPONSE`), `BackoffDelay(base, attempt)` — `base·2^(n-1)`
  capped at `MaxRetryBackoff` (4 s).

### `backend/internal/ai/health.go`

#### Primary Role
`CheckProvider(ctx, cfg, client)` — actionable pre-flight checks before
any generation.

#### Algorithms & Logic
- Always: cancellation check + `cfg.Validate()`.
- `ollama`: derive native root from the (possibly OpenAI-compatible) base
  URL (`/v1` stripped) → `GET <root>/api/tags` (unreachable →
  `PROVIDER_UNAVAILABLE` *retryable*; bad status → typed with 429/503
  retryable) → compare the wanted model against installed ones with
  `normalizeOllamaModel` (implicit `:latest` tag so `qwen2` ==
  `qwen2:latest`) → missing → `MODEL_MISSING` listing installed models.
- `gemini`/`groq`: API key presence (`API_KEY_MISSING`).
- Unknown: `PROVIDER_UNSUPPORTED` listing `ListProviders()`. Keys never
  appear in returned errors.

### `backend/internal/ai/validate.go`

#### Primary Role
Response-schema enforcement and the retry loop.

#### Key Structures
- `aiResolutionWire{Explanation, SuggestedCode, ConfidenceScore
  *pointers}` — pointer fields distinguish *missing* from zero during
  parse (snake_case internal schema; camelCase translation happens only
  at the API boundary).
- `ValidateResponse(res)` — non-empty explanation + suggested_code,
  confidence 0–100; violations are typed **non-retryable**
  `SCHEMA_INVALID`.
- `parseJSONResponse(rawText)` — strips ```` ```json ```` fences (small
  local models ignore `response_format`), unmarshals into the wire type
  (missing field → `SCHEMA_INVALID`; unparseable → `MALFORMED_JSON`;
  empty → `EMPTY_RESPONSE`), then `ValidateResponse`.
- `ResolveCollisionWithRetry(ctx, cfg, resolver, collision, promptCtx)`
  — raw context errors returned untouched (`errors.Is` contract);
  prompt-size pre-check (`len(promptCtx.Context) > MaxPromptSize` →
  `PROMPT_TOO_LARGE` *before any network traffic*); loop
  `attempt ≤ RetryCount`: cancellation-aware backoff timer → per-attempt
  `context.WithTimeout(ctx, RequestTimeout)` → `ResolveCollision` →
  success validated; parent cancellation always wins over provider
  errors; retry **only** when the typed error is `Retryable`.

### `backend/internal/ai/ollama.go`

#### Primary Role
Ollama provider + the shared OpenAI-compatible request flow.

#### Key Structures
- `init()` registers `ollama`; `OllamaResolver{BaseURL, Model, Client,
  APIKey, MaxResponseSize}`; `newOllamaResolver` (empty base URL →
  documented default; API key retained only for redaction — never sent).
- `ResolveCollision` → `resolveOpenAICompatible(...)`: system prompt +
  `promptCtx.Context + "\n\n" + JSON(collision)` user message,
  `temperature: 0.0`, `response_format: json_object`, `POST
  <base>/chat/completions`, `Authorization: Bearer` only when a key
  exists, then `performRequest` → `extractMessageContent` →
  `parseJSONResponse`.

### `backend/internal/ai/gemini.go` / `groq.go`

#### Key Structures
- `init()` registers `gemini` / `groq`.
- `GeminiResolver` — requires API key at construction; request is
  `systemInstruction` + two user `contents` parts, `responseMimeType:
  application/json`, `temperature: 0.0`; the key travels in the
  **`x-goog-api-key` header** (never in the URL, so `url.Error` can't
  leak it); response parsed from `candidates[0].content.parts[0].text`
  (empty → `EMPTY_RESPONSE`) → `parseJSONResponse`.
- `GroqResolver` — defaults to `DefaultGroqBaseURL`; delegates to
  `resolveOpenAICompatible` with the API key as both auth and
  redaction secret.

## Cross-Module Flow

- `engine.runAIResolution` → `GetResolver(cfg)` + `CheckProvider` → per
  collision `ResolveCollisionWithRetry` → `AIResolutionResponse` →
  `runstate.Suggestion` (+ `resolutions.Resolution` on success).
- `internal/suggestions` runs the same trio concurrently with a
  bounded worker pool; the API surfaces `ai.ErrorCode`s as typed HTTP
  errors.
- `cmd/resolve.go` assembles `Config` (flags > env > defaults) and
  `cmd/serve.go`/API read sanitized metadata (`Sanitized()`) for
  `/api/suggestions` responses.

## Tests

- `backend/internal/ai/config_test.go` — defaults per provider,
  env parsing, precedence (`ConfigFromEnv` + `ApplyProviderDefaults`),
  validation order/messages, `ValidateBaseURL`, `Sanitized`.
- `backend/internal/ai/health_test.go` — `CheckProvider` branches
  (Ollama reachability/model normalization, API-key requirements,
  unsupported provider) against a test HTTP server.
- `backend/internal/ai/validate_test.go` — schema validation, fenced-JSON
  tolerance, missing-field detection via pointer wire type, retry policy
  (retryable vs not, backoff bounds, cancellation never retried, prompt
  size cap).
