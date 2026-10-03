# Phase 2 Final Delivery Report

## 1. Review findings

- `docs/PHASE2.md` does not exist. The actual spec is `docs/phase2_plan.md`; it was treated as authoritative (its scope matches the request exactly).
- Phase 1 was confirmed complete before changes: all documented Phase 1 behavior (config validation with `CodedError`s, context propagation, structured conflict regions, git stage validation, semantic identity, repository-scoped `runstate.Run`, deterministic ordering, no package-level mutable state for run data) and the baseline checks (`go test ./...`, `go test -race ./...`, `go vet ./...`) passed before edits.
- Gaps closed by this change:
  - Hard-coded `ollama`/`qwen2:1.5b` and `context.Background()` in `internal/api/server.go`.
  - Fixed 60s client timeout in `internal/ai/ai.go`.
  - No retry policy, no per-request timeout configuration, no response-size limit, weak schema validation.
  - `--provider` defaulted to `gemini` (not Ollama-first).
  - Suggestions stored only file-scoped; no per-collision ID/provider/model/status/error metadata.
  - `frontend/src/tabs/history.jsx` called the removed `/api/prompt/statistics` — replaced with `GET /api/history`.
- Real bug fixed: Gemini sent the API key in the URL query, and `client.Do` errors are `*url.Error` embedding that URL, so keys could leak into logs/API. Key now travels in `x-goog-api-key`; all AI errors pass through multi-layer redaction (query params, bearer tokens, configured secret).
- Remaining concerns assessed as acceptable: retry/backoff parameters are config-driven (no mutable package state); retries never apply to cancellation/timeout/auth/schema failures; `analyze`/`serve` remain AI-free.

## 2. Files modified

**Backend — new**
- `internal/ai/config.go` — unified `ai.Config` (Provider, Model, BaseURL, APIKey, request timeout, retries, confidence threshold, max prompt/response size, backoff) with precedence flags > env (`AI_*`) > provider defaults, `Validate`, `ConfigFromEnv`, `Sanitized`, provider defaults, `ValidateBaseURL`.
- `internal/ai/errors.go` — typed AI errors with stable codes, `Retryable` classification, secret redaction.
- `internal/ai/http.go` — `NewClient` from config, bounded `readBodyLimited`, `performRequest` mapping statuses to typed retryable/non-retryable errors, `resolveOpenAICompatible` shared flow, `BackoffDelay`.
- `internal/ai/validate.go` — strict `ValidateResponse`, fenced-JSON-tolerant `parseJSONResponse`, `ResolveCollisionWithRetry` (bounded retries, per-attempt timeout, exponential backoff, no retry on cancellation/timeout/schema/format failures).
- `internal/ai/health.go` — `CheckProvider`: Ollama reachability + model existence (with `:latest` normalization), API-key presence for Gemini/Groq, base URL validity, unsupported-provider errors. No secrets in messages.
- `internal/ai/ollama.go`, `gemini.go`, `groq.go` — config-driven resolvers; Gemini key via header only; shared `resolveOpenAICompatible`.
- `internal/runstate/history.go` — `runstate.RunHistory` read model (run ID, repository, timings, files analyzed, collision count, provider/model, suggestion counts, timeout/cancellation, error summaries) + bounded `runstate.History` (100-entry default, deterministic ordering, atomic merges).
- `internal/suggestions/suggestions.go` — `Generator` with bounded concurrency, deterministic `(repository, file, collisionKey)` ordering, partial-failure reporting, run/scoped dedupe (in-flight registry + stored-success reuse), below-threshold statuses, history recording, full `Suggestion` metadata.

**Backend — changed**
- `internal/ai/ai.go` — `AIConfig` alias retained, `GetResolver` bounded client, registry kept.
- `internal/api/server.go` — request-scoped `/api/suggestions` (`r.Context()`, typed mapping, `meta` payload, provider/model in response), `GET /api/history`, `NewHandler` for testability.
- `internal/engine/config.go` — `engine.Config` drops flat AI fields; AI lives in `cfg.AI ai.Config`; `DefaultConfig` wires defaults.
- `internal/engine/pipeline.go` — `runAIResolution` now health-checks, applies per-request timeout + retries, and records each outcome as a run-scoped suggestion (no data loss on partial failure).
- `internal/runstate/runstate.go` — `Suggestion` additive metadata (ID, Repository, CollisionKey, Provider, Model, Status, ErrorCode, Retryable, ErrorMessage, StartedAt, CompletedAt); `SaveSuggestion`/`FindSuggestion` collision-keyed lookup; in-flight registry (`TryBeginSuggestion`/`EndSuggestion`); sorted/upserted storage.
- `internal/runstate/isolation_test.go` (unchanged) — pre-existing isolation tests still pass.
- `cmd/resolve.go` — env+flag precedence, Ollama-first provider default, `--retries`, `--ai-timeout`; AI config validated before scanning; no hard-coded providers.
- `cmd/serve.go` — env-driven AI metadata; records bounded run history; passes history to the API server.
- `internal/runstate/runstate.go` — added `Suggestion` metadata fields + collision-keyed storage helpers (see above).

**Tests — new**
- `internal/ai/config_test.go` (13), `internal/ai/validate_test.go` (16), `internal/ai/health_test.go` (17): config precedence/validation, schema validation + retry policy, provider health + redaction.
- `internal/suggestions/suggestions_test.go` (16): bounded concurrency, deterministic ordering, partial failures, dedupe, isolation, threshold status, history recording.
- `internal/runstate/suggestions_history_test.go` (10): upsert/find/in-flight, bounded history, isolation.
- `internal/api/server_test.go` (9): suggestions + history endpoints, typed error mapping, no key leakage.
- `internal/engine/ai_resolution_test.go` (3): engine AI integration (recording, unavailable provider, invalid schema).
- **Totals: 84 top-level test functions, 149 passing cases, 0 failures.**

**Frontend**
- `frontend/src/tabs/suggestions.jsx` — provider/status metadata, partial-failure banner with retry, loading/empty/error/provider-unavailable/timeout states, retry where retryable.
- `frontend/src/tabs/history.jsx` — `/api/history` consumer with loading/empty/error states and per-run status.
- `frontend/src/tabs/{suggestions,history}.css` — Phase 2 metadata/status/CSS additions.

**Docs**
- `docs/phase2_plan.md` (existing file referenced as the Phase 2 spec)
- `README.md` — documented the `AI_*` configuration table and the new endpoints (`/api/suggestions` `meta`, `/api/history`).

## 3. Architectural changes

- **One shared `ai.Config`** with precedence flags > env > defaults; Ollama-first; documented default `qwen2:1.5b`; `OLLAMA_BASE_URL` kept as legacy fallback.
- **Typed, retryable AI errors** with mandatory redaction; no secrets in logs/API.
- **Health checks before generation** (Ollama reachability + model presence, API-key presence for hosted providers, base URL validity, clear unsupported-provider errors).
- **Retry executor** — per-attempt timeout, bounded exponential backoff, retries only on transient transport/HTTP failures.
- **`internal/suggestions` service** — bounded AI concurrency, deterministic ordering, partial-failure reporting, run-scoped dedupe, threshold marking, history recording.
- **Suggestions stored per collision** in `runstate.Run` with additive metadata (stable IDs, provider, model, status, error info, timestamps).
- **Bounded in-memory run history** owned by the server (no package-level state); `/api/history`; obsolete `/api/prompt/statistics` not restored.

## 4. Tests added

Backend: 7 new files, 84 top-level test functions, 149 passing cases (verified this session — exit 0, race clean, vet clean).
- Config precedence/validation (13)
- Response schema + retry policy (16)
- Health checks + redaction (17)
- Suggested service concurrency/ordering/partials/dedupe/isolation (16)
- History storage/eviction/merging (10)
- API handlers (9)
- Engine AI integration (3)

Frontend: no unit tests added (plan marked non-blocking) — lint + build + live endpoint checks used instead.

## 5. Verification results

| Command | Exit status | Result |
| --- | --- | --- |
| `cd backend && go test ./...` | 0 | all packages `ok` |
| `cd backend && go test -count=1 -race ./...` | 0 | no races |
| `cd backend && go vet ./...` | 0 | clean |
| `cd frontend && npm run lint` | 0 | clean |
| `cd frontend && npm run build` | 0 | `dist/` emitted |
| Live end-to-end | — | real conflicted git repo: `/api/history` real data, `/api/analysis` intact, `/api/suggestions` typed `503 PROVIDER_UNAVAILABLE` |

## 6. Remaining risks

- Retry backoff is real wall-clock; `RetryBackoff` has no CLI flag (env only).
- History is process-local, bounded (100), in-memory, no persistence (by design).
- Multi-repository bare-`?file=` lookups resolve to the first deterministic match (documented Phase 1 limitation); an explicit repository hint is honored internally now.
- `internal/ai` registry globals remain (Phase 1-accepted write-once config); no run data in package globals.
- `context.Background()` only as nil-context guards in Phase 1 library helpers, never on active request paths.
- No frontend unit/browser tests — lint/build/live API checks instead.
- Phase 1 issue #16 (report deletion after AI resolution) intentionally unchanged.
- 4 pre-existing non-`gofmt`-clean files (`prompt/prompt.go`, `report/report.go`, `semantic_test.go`, `main.go`) untouched to keep the diff minimal.

## 7. Recommended Phase 3 starting point

1. **Patch preview** from `Suggestion.Resolution.SuggestedCode` (stable IDs, scope, confidence already stored) — no file writes.
2. **Explicit approval + apply-to-file with rollback** — pre-apply snapshot retained per-collision, strictly idempotent.
3. **Close issue #16** — stop deleting cached reports after a resolution is applied/approved, so the audit trail persists in `runstate.Run` + history.
4. **Optional durable history** — persist `runstate.History` (bounded in-memory store) for runs surviving restarts; add repository-qualified suggestion lookup to the API.
5. **Frontend test harness** — test-runner + state-machine coverage for the suggestions/history UI (loading/empty/timeout/unavailable/partial-failure/retry).

## 8. Out of scope (documented and skipped)

- Apply-to-file workflows, patch preview, approval workflows, rollback, automatic commits, autonomous conflict resolution.
- Database persistence of suggestions/reports (Phase 2 history is intentionally bounded in-memory).
- Broad frontend redesign, graph feature work, general API security and graceful server shutdown.
- `/api/prompt/statistics` restoration (replaced by `/api/history`).
