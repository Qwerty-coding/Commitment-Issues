// Package resolutions defines the Phase 3 resolution data model: a
// patch proposal linked to a single run-scoped suggestion, plus the
// immutable audit events for its lifecycle.
//
// The package is deliberately free of internal dependencies: it holds
// only data, statuses, identifiers and the stable error-code contract.
// All mutable, run-scoped state lives in internal/runstate, which
// imports this package (never the reverse), so no import cycle can
// form as later phases add patch building and validation.
package resolutions

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Lifecycle statuses of a resolution.
const (
	// StatusProposed is the initial state of a generated resolution.
	StatusProposed = "proposed"
	// StatusPreviewed marks a resolution whose read-only preview
	// has been generated.
	StatusPreviewed = "previewed"
	// StatusApproved marks a resolution with explicit user approval.
	StatusApproved = "approved"
	// StatusApplied marks a resolution whose patch was applied.
	StatusApplied = "applied"
	// StatusReverted marks a resolution whose applied patch was
	// rolled back to its pre-apply content.
	StatusReverted = "reverted"
	// StatusStale marks a resolution invalidated by a changed file,
	// changed conflict region, changed repository identity or a
	// regenerated (re-revisioned) suggestion.
	StatusStale = "stale"
	// StatusFailed marks a resolution whose apply or revert failed.
	StatusFailed = "failed"
	// StatusValidationFailed marks a resolution that was applied but
	// whose post-apply validation failed.
	StatusValidationFailed = "validation_failed"
	// StatusManualReview marks a resolution requiring manual review because
	// the conflict region cannot be safely mapped or language is unsupported.
	StatusManualReview = "manual_review"
)

// Approval statuses of a resolution. Approval is explicit and expires
// when the source file, conflict region, repository identity or
// suggestion revision changes.
const (
	// ApprovalNone marks a resolution that was never approved.
	ApprovalNone = "none"
	// ApprovalApproved marks an explicit approval that is still valid.
	ApprovalApproved = "approved"
	// ApprovalExpired marks an approval invalidated by a change to
	// the suggestion or the analysed file.
	ApprovalExpired = "expired"
)

// Post-apply validation statuses.
const (
	// ValidationNotRun marks a resolution with no validation result
	// yet (including applied resolutions where validation was not
	// configured).
	ValidationNotRun = "not_run"
	// ValidationRunning marks a validation command in progress.
	ValidationRunning = "running"
	// ValidationPassed marks a successful validation command.
	ValidationPassed = "passed"
	// ValidationFailed marks a validation command that exited
	// non-zero. The applied change is preserved; it is never
	// reverted automatically.
	ValidationFailed = "failed"
	// ValidationTimedOut marks a validation command that exceeded
	// its deadline.
	ValidationTimedOut = "timed_out"
	// ValidationCancelled marks a validation command cancelled via
	// context cancellation.
	ValidationCancelled = "cancelled"
)

// Audit event types. Events are immutable once recorded.
const (
	// EventProposed records explicit creation or proposal of a resolution.
	EventProposed = "proposed"
	// EventPreviewed records read-only preview generation.
	EventPreviewed = "previewed"
	// EventApproved records explicit user approval.
	EventApproved = "approved"
	// EventApplyStarted records the beginning of an apply attempt.
	EventApplyStarted = "apply_started"
	// EventApplied records a successful atomic apply.
	EventApplied = "applied"
	// EventValidationStarted records the beginning of a validation run.
	EventValidationStarted = "validation_started"
	// EventValidationPassed records a successful validation run.
	EventValidationPassed = "validation_passed"
	// EventValidationFailed records a failed validation run.
	EventValidationFailed = "validation_failed"
	// EventRevertStarted records the beginning of a revert attempt.
	EventRevertStarted = "revert_started"
	// EventReverted records a successful rollback.
	EventReverted = "reverted"
	// EventStale records a resolution becoming stale.
	EventStale = "stale"
	// EventFailed records a failed apply or revert.
	EventFailed = "failed"
)

// Actor values identify the source that triggered an event.
const (
	// ActorUser marks events triggered by an explicit user action
	// through the API or UI.
	ActorUser = "user"
	// ActorCLI marks events triggered by a CLI invocation.
	ActorCLI = "cli"
	// ActorSystem marks events triggered by the tool itself (for
	// example staleness invalidation).
	ActorSystem = "system"
)

// ErrorCode is a stable, machine-readable identifier for a resolution
// failure. Codes are part of the API contract: the frontend maps them
// to user-facing states (stale file, approval required, ...).
type ErrorCode string

const (
	// CodeResolutionNotFound indicates no resolution exists for the
	// supplied identifier.
	CodeResolutionNotFound ErrorCode = "RESOLUTION_NOT_FOUND"
	// CodeNotApproved indicates a mutation was attempted without
	// explicit approval.
	CodeNotApproved ErrorCode = "NOT_APPROVED"
	// CodeAlreadyApplied indicates the resolution was already applied.
	CodeAlreadyApplied ErrorCode = "ALREADY_APPLIED"
	// CodeStaleFile indicates the working-tree file or its conflict
	// region changed since analysis; the user must re-analyze.
	CodeStaleFile ErrorCode = "STALE_FILE"
	// CodeInvalidPatch indicates the proposed replacement cannot be
	// safely mapped to the stored conflict region.
	CodeInvalidPatch ErrorCode = "INVALID_PATCH"
	// CodePathEscape indicates the target path leaves the repository
	// root (including Windows drive/path escapes).
	CodePathEscape ErrorCode = "PATH_ESCAPE"
	// CodeRepositoryMismatch indicates the repository identity no
	// longer matches the analysis-time repository.
	CodeRepositoryMismatch ErrorCode = "REPOSITORY_MISMATCH"
	// CodeApprovalExpired indicates the approval was invalidated by a
	// suggestion regeneration or a changed source.
	CodeApprovalExpired ErrorCode = "APPROVAL_EXPIRED"
	// CodeValidationFailure indicates a post-apply validation command
	// failed. The applied change is preserved.
	CodeValidationFailure ErrorCode = "VALIDATION_FAILURE"
	// CodeRollbackFailure indicates a revert could not be performed
	// safely (for example after unrelated user edits).
	CodeRollbackFailure ErrorCode = "ROLLBACK_FAILURE"
	// CodeApplyInProgress indicates another apply for the same
	// resolution is still in flight.
	CodeApplyInProgress ErrorCode = "APPLY_IN_PROGRESS"
	// CodeInvalidTransition indicates an invalid lifecycle state transition.
	CodeInvalidTransition ErrorCode = "INVALID_TRANSITION"
)

// Resolution is one patch proposal linked to a single run-scoped
// suggestion. It is created at proposal time, extended by preview,
// approval, apply, validation and revert, and never loses its
// original audit trail.
type Resolution struct {
	// ID is the opaque, URL-safe identifier used in API paths
	// (/api/resolutions/{id}). It is generated by NewID and never
	// contains repository paths or "|" characters.
	ID string `json:"id"`
	// SuggestionID is the stable suggestion identity this resolution
	// was generated from (<repository>|<file>|<collisionKey>).
	SuggestionID string `json:"suggestionId"`
	// RunID is the analysis run that produced the suggestion.
	RunID string `json:"runId"`
	// Repository is the canonical repository root the resolution
	// belongs to.
	Repository string `json:"repository"`
	// File is the repository-relative path of the conflicted file.
	File string `json:"file"`
	// CollisionKey is the stable per-collision identity (semantic
	// DiffKey) the resolution was generated for.
	CollisionKey string `json:"collisionKey,omitempty"`
	// Revision is the suggestion revision this resolution was built
	// from. It is invalidated when the suggestion is regenerated.
	Revision int `json:"revision,omitempty"`
	// RegionID identifies the exact conflict region within the file
	// that the replacement applies to.
	RegionID string `json:"regionId,omitempty"`
	// StartLine and EndLine are the 1-based inclusive line range of
	// the conflict region in the working-tree file.
	StartLine int `json:"startLine,omitempty"`
	EndLine   int `json:"endLine,omitempty"`
	// StartOffset and EndOffset are the byte offsets of the conflict
	// region in the working-tree file.
	StartOffset int64 `json:"startOffset,omitempty"`
	EndOffset   int64 `json:"endOffset,omitempty"`
	// ContentHash is the hash of the working-tree file at analysis
	// time — the exact file that will be modified.
	ContentHash string `json:"contentHash,omitempty"`
	// OriginalContentHash is the SHA-256 hash of the working-tree file immediately
	// before apply.
	OriginalContentHash string `json:"originalContentHash,omitempty"`
	// ContextHash is the hash of the conflict region plus its
	// surrounding context lines at analysis time.
	ContextHash string `json:"contextHash,omitempty"`
	// Base, Ours and Theirs are the stored conflict-region contents
	// from analysis.
	Base   string `json:"base,omitempty"`
	Ours   string `json:"ours,omitempty"`
	Theirs string `json:"theirs,omitempty"`
	// Replacement is the proposed region replacement content, derived
	// from the AI suggestion and validated against the stored region.
	Replacement string `json:"replacement,omitempty"`
	// PreviewDiff is the read-only unified diff produced at preview
	// time. It is informational and never implies a write.
	PreviewDiff string `json:"previewDiff,omitempty"`
	// PreApplyContent is the exact working-tree file content immediately
	// before apply, saved for rollback.
	PreApplyContent string `json:"preApplyContent,omitempty"`
	// PostApplyHash is the SHA-256 hash of the working-tree file immediately
	// after apply, used to detect subsequent edits before rollback.
	PostApplyHash string `json:"postApplyHash,omitempty"`
	// PostApplyContentHash is the SHA-256 hash of the working-tree file
	// immediately after apply.
	PostApplyContentHash string `json:"postApplyContentHash,omitempty"`
	// Status is the lifecycle status (see the Status* constants).
	Status string `json:"status"`
	// ApprovalStatus is the explicit approval state (none, approved,
	// expired).
	ApprovalStatus string `json:"approvalStatus,omitempty"`
	// ValidationStatus is the post-apply validation state (see the
	// Validation* constants).
	ValidationStatus string `json:"validationStatus,omitempty"`
	// ValidationOutput is the captured stdout+stderr from post-apply validation.
	ValidationOutput string `json:"validationOutput,omitempty"`
	// ValidationExitCode is the process exit code from post-apply validation.
	ValidationExitCode int `json:"validationExitCode,omitempty"`
	// ValidationDuration is the runtime duration of the post-apply validation run.
	ValidationDuration time.Duration `json:"validationDuration,omitempty"`
	// ApprovedAt, AppliedAt and RevertedAt bound the explicit
	// mutation timestamps (UTC, zero when not yet reached).
	ApprovedAt time.Time `json:"approvedAt,omitempty"`
	AppliedAt  time.Time `json:"appliedAt,omitempty"`
	RevertedAt time.Time `json:"revertedAt,omitempty"`
	// CreatedAt is the proposal timestamp (UTC).
	CreatedAt time.Time `json:"createdAt"`
	// ErrorCode and ErrorMessage carry the typed, redacted failure
	// information of the last failed operation.
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ResolutionEvent is one immutable audit record. Events are appended
// once and never modified; the sequence number is assigned by the
// run-scoped store under its lock.
type ResolutionEvent struct {
	// Sequence is the run-wide monotonic event order.
	Sequence int64 `json:"sequence"`
	// ResolutionID links the event to its resolution.
	ResolutionID string `json:"resolutionId"`
	// RunID, Repository and File qualify the event so events from
	// identical paths in different repositories never collide.
	RunID      string `json:"runId"`
	Repository string `json:"repository"`
	File       string `json:"file"`
	// Timestamp is the event time (UTC).
	Timestamp time.Time `json:"timestamp"`
	// Actor identifies the source (user, cli or system).
	Actor string `json:"actor"`
	// Type is the event type (see the Event* constants).
	Type string `json:"type"`
	// PreviousStatus and NewStatus record the lifecycle transition.
	PreviousStatus string `json:"previousStatus,omitempty"`
	NewStatus      string `json:"newStatus"`
	// ErrorCode carries the typed error code when the event records
	// a failure.
	ErrorCode string `json:"errorCode,omitempty"`
}

// NewID returns a new opaque, URL-safe resolution identifier (32
// lowercase hex characters). It never contains path separators, pipes,
// colons or other characters that are unsafe in a URL path segment, so
// it can be embedded directly in /api/resolutions/{id} routes without
// leaking absolute Windows paths or repository layout.
func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is effectively impossible; fall back to a
		// time-based identifier that is still opaque and URL-safe.
		return fmt.Sprintf("res%016x", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// ValidateTransition checks if transitioning from fromStatus to toStatus is valid.
func ValidateTransition(fromStatus, toStatus string) error {
	if fromStatus == toStatus {
		return nil
	}
	transitions := map[string]map[string]bool{
		StatusProposed: {
			StatusPreviewed:    true,
			StatusApproved:     true,
			StatusStale:        true,
			StatusFailed:       true,
			StatusManualReview: true,
		},
		StatusPreviewed: {
			StatusApproved:     true,
			StatusStale:        true,
			StatusFailed:       true,
			StatusManualReview: true,
		},
		StatusApproved: {
			StatusPreviewed: true,
			StatusApplied:   true,
			StatusStale:     true,
			StatusFailed:    true,
		},
		StatusApplied: {
			StatusValidationFailed: true,
			StatusReverted:         true,
			StatusFailed:           true,
		},
		StatusValidationFailed: {
			StatusReverted: true,
			StatusFailed:   true,
		},
		StatusManualReview: {
			StatusStale:  true,
			StatusFailed: true,
		},
	}
	allowed, ok := transitions[fromStatus]
	if !ok || !allowed[toStatus] {
		return fmt.Errorf("invalid lifecycle transition from %s to %s", fromStatus, toStatus)
	}
	return nil
}
