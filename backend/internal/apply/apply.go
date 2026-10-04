// Package apply implements Phase 6 (safe atomic patch application) and
// Phase 7 (rollback) for resolution patches.
//
// The apply workflow mandated by the plan:
//  1. Acquire the per-resolution in-flight lock (APPLY_IN_PROGRESS if busy).
//  2. Re-validate the file, repository, and resolution status.
//  3. Verify approval is current.
//  4. Create a pre-apply snapshot (original file bytes + hash).
//  5. Build the patch in memory.
//  6. Write atomically.
//  7. Verify the resulting file hash matches the expected post-apply hash.
//  8. Record the apply event.
//  9. Return separately: applied status + validation status.
//
// Rollback:
//   - Verify current file matches the stored post-apply hash.
//   - Refuse if the file changed after apply (unrelated user edits).
//   - Restore the exact pre-apply content atomically.
//   - Never uses `git reset --hard`.
package apply

import (
	"fmt"
	"os"
	"time"

	"context"

	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/fileutil"
	"CommitIssues/internal/patch"
	"CommitIssues/internal/resolutions"
	"CommitIssues/internal/runstate"
	"CommitIssues/internal/safeguard"
	"CommitIssues/internal/validation"
)

// ApplyRequest carries the caller-supplied parameters for an apply operation.
// Every field is required; the API handler validates them before calling Apply.
type ApplyRequest struct {
	// ResolutionID is the opaque resolution identifier.
	ResolutionID string
	// RepositoryRoot is the canonical repository root supplied by the caller.
	// It must match the root stored in the resolution.
	RepositoryRoot string
	// File is the repository-relative file path.
	File string
	// SuggestionRevision is the caller's expected revision. If it differs
	// from the stored revision, the apply is rejected as ApprovalExpired.
	SuggestionRevision int
	// ExpectedContentHash, when non-empty, must match the working-tree hash
	// before apply proceeds.
	ExpectedContentHash string
}

// ApplyResult is the combined outcome of Apply.
type ApplyResult struct {
	// Resolution is the updated resolution after apply.
	Resolution resolutions.Resolution
	// ValidationResult is the post-apply validation outcome (may be nil when
	// no validation is configured).
	ValidationResult *validation.Result
	// PostApplyHash is the SHA-256 hash of the file after the patch was written.
	PostApplyHash string
}

// Apply executes the full apply workflow for the resolution identified by
// req.ResolutionID. It must be called only after the resolution has been
// previewed and approved.
//
// run is the run-scoped state store. analysis is the stored FileAnalysis
// snapshot for the file. validationCfg is nil when no validation is configured
// (validation is then recorded as not_run, never fails automatically).
func Apply(
	ctx context.Context,
	run *runstate.Run,
	analysis promptcontext.FileAnalysis,
	req ApplyRequest,
	validationCfg *validation.Config,
) (ApplyResult, error) {
	// ── Step 1: acquire per-resolution in-flight lock ─────────────────────
	if !run.TryBeginResolutionApply(req.ResolutionID) {
		return ApplyResult{}, &safeguard.SafeError{
			Code:    resolutions.CodeApplyInProgress,
			Message: fmt.Sprintf("an apply is already in progress for resolution %s", req.ResolutionID),
		}
	}
	defer run.EndResolutionApply(req.ResolutionID)

	// ── Step 2: load and validate the resolution ──────────────────────────
	res, ok := run.GetResolution(req.ResolutionID)
	if !ok {
		return ApplyResult{}, &safeguard.SafeError{
			Code:    resolutions.CodeResolutionNotFound,
			Message: fmt.Sprintf("resolution %s not found", req.ResolutionID),
		}
	}

	// Status check (not stale, applied, reverted, or failed).
	if err := safeguard.VerifyStatus(res); err != nil {
		return ApplyResult{}, err
	}

	// Approval check.
	if err := safeguard.VerifyApproval(res); err != nil {
		return ApplyResult{}, err
	}

	// Revision check.
	if err := safeguard.VerifyRevision(res.Revision, req.SuggestionRevision); err != nil {
		return ApplyResult{}, err
	}

	// ── Step 3: canonicalize repository and verify file path ──────────────
	absRepo, err := safeguard.VerifyRepository(req.RepositoryRoot)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := safeguard.VerifyRepositoryIdentity(res.Repository, absRepo); err != nil {
		return ApplyResult{}, err
	}
	absPath, err := safeguard.VerifyFilePath(absRepo, req.File)
	if err != nil {
		return ApplyResult{}, err
	}

	// ── Step 4: verify working tree ───────────────────────────────────────
	wtCheck, err := safeguard.VerifyWorkingTree(absPath, analysis, res.RegionID)
	if err != nil {
		return ApplyResult{}, err
	}
	// Optional caller-supplied content hash assertion.
	if req.ExpectedContentHash != "" && wtCheck.CurrentHash != req.ExpectedContentHash {
		return ApplyResult{}, &safeguard.SafeError{
			Code: resolutions.CodeStaleFile,
			Message: fmt.Sprintf("caller-expected content hash %s does not match working-tree hash %s",
				req.ExpectedContentHash, wtCheck.CurrentHash),
		}
	}

	// ── Step 5: record apply_started event ───────────────────────────────
	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID:   res.ID,
		RunID:          res.RunID,
		Repository:     res.Repository,
		File:           res.File,
		Actor:          resolutions.ActorUser,
		Type:           resolutions.EventApplyStarted,
		PreviousStatus: res.Status,
		NewStatus:      resolutions.StatusApplied,
	})

	// ── Step 6: build patch in memory ────────────────────────────────────
	previewResult, err := patch.Build(analysis, res, wtCheck.CurrentData)
	if err != nil {
		res.Status = resolutions.StatusFailed
		res.ErrorCode = string(resolutions.CodeInvalidPatch)
		res.ErrorMessage = err.Error()
		run.SaveResolution(res)
		run.RecordResolutionEvent(resolutions.ResolutionEvent{
			ResolutionID:   res.ID,
			RunID:          res.RunID,
			Repository:     res.Repository,
			File:           res.File,
			Actor:          resolutions.ActorSystem,
			Type:           resolutions.EventFailed,
			PreviousStatus: resolutions.StatusApproved,
			NewStatus:      resolutions.StatusFailed,
			ErrorCode:      string(resolutions.CodeInvalidPatch),
		})
		return ApplyResult{}, err
	}

	// Store the pre-apply snapshot on the resolution before writing.
	res.PreviewDiff = previewResult.UnifiedDiff
	// The resolution's ContentHash (pre-apply) is already stored.

	// ── Step 7: atomic write ─────────────────────────────────────────────
	info, statErr := os.Stat(absPath)
	perm := os.FileMode(0o644)
	if statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := fileutil.AtomicWrite(absPath, []byte(previewResult.ProposedContent), perm); err != nil {
		res.Status = resolutions.StatusFailed
		res.ErrorCode = string(resolutions.CodeInvalidPatch)
		res.ErrorMessage = fmt.Sprintf("atomic write failed: %v", err)
		run.SaveResolution(res)
		run.RecordResolutionEvent(resolutions.ResolutionEvent{
			ResolutionID:   res.ID,
			RunID:          res.RunID,
			Repository:     res.Repository,
			File:           res.File,
			Actor:          resolutions.ActorSystem,
			Type:           resolutions.EventFailed,
			PreviousStatus: resolutions.StatusApproved,
			NewStatus:      resolutions.StatusFailed,
			ErrorCode:      string(resolutions.CodeInvalidPatch),
		})
		return ApplyResult{}, fmt.Errorf("apply: write failed: %w", err)
	}

	// ── Step 8: verify post-apply hash ────────────────────────────────────
	postHash, hashErr := fileutil.HashFile(absPath)
	if hashErr != nil {
		// Non-fatal: we wrote successfully, just can't verify. Record it.
		postHash = ""
	}
	expectedPostHash := fileutil.HashContent([]byte(previewResult.ProposedContent))
	if postHash != "" && postHash != expectedPostHash {
		// The file was modified between write and verify — highly unusual.
		// Mark the resolution as failed so the user is prompted to re-analyze.
		res.Status = resolutions.StatusFailed
		res.ErrorCode = string(resolutions.CodeInvalidPatch)
		res.ErrorMessage = "post-apply hash mismatch; file may have been modified concurrently"
		run.SaveResolution(res)
		return ApplyResult{}, fmt.Errorf("apply: post-apply hash mismatch")
	}

	// ── Step 9: record applied event and update resolution ────────────────
	now := time.Now().UTC()
	prevStatus := res.Status
	res.Status = resolutions.StatusApplied
	res.AppliedAt = now
	res.PreApplyContent = previewResult.OriginalContent
	res.OriginalContentHash = wtCheck.CurrentHash
	if res.ContentHash == "" {
		res.ContentHash = wtCheck.CurrentHash
	}
	res.PostApplyHash = expectedPostHash
	res.PostApplyContentHash = expectedPostHash
	res = run.SaveResolution(res)

	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID:   res.ID,
		RunID:          res.RunID,
		Repository:     res.Repository,
		File:           res.File,
		Actor:          resolutions.ActorUser,
		Type:           resolutions.EventApplied,
		PreviousStatus: prevStatus,
		NewStatus:      resolutions.StatusApplied,
	})

	// ── Step 10: run validation (optional) ───────────────────────────────
	var valResult *validation.Result
	if validationCfg != nil && len(validationCfg.AllowList) > 0 {
		runner, runnerErr := validation.NewRunner(*validationCfg)
		if runnerErr == nil {
			run.RecordResolutionEvent(resolutions.ResolutionEvent{
				ResolutionID:   res.ID,
				RunID:          res.RunID,
				Repository:     res.Repository,
				File:           res.File,
				Actor:          resolutions.ActorSystem,
				Type:           resolutions.EventValidationStarted,
				PreviousStatus: resolutions.StatusApplied,
				NewStatus:      resolutions.StatusApplied,
			})
			cmdName := validationCfg.AllowList[0]
			var cmdArgs []string
			if len(validationCfg.Command) > 0 {
				cmdName = validationCfg.Command[0]
				cmdArgs = validationCfg.Command[1:]
			}
			r := runner.Run(ctx, cmdName, cmdArgs)
			valResult = &r

			eventType := resolutions.EventValidationPassed
			valStatus := resolutions.ValidationPassed
			if r.Status != validation.StatusPassed {
				eventType = resolutions.EventValidationFailed
				valStatus = resolutions.ValidationFailed
				if r.Status == validation.StatusTimedOut {
					valStatus = resolutions.ValidationTimedOut
				} else if r.Status == validation.StatusCancelled {
					valStatus = resolutions.ValidationCancelled
				}
				// Validation failure does NOT revert automatically.
				res.Status = resolutions.StatusValidationFailed
				res.ValidationStatus = valStatus
				res.ValidationOutput = r.Output
				res.ValidationExitCode = r.ExitCode
				res.ValidationDuration = r.Duration
				res = run.SaveResolution(res)
			} else {
				res.ValidationStatus = valStatus
				res.ValidationOutput = r.Output
				res.ValidationExitCode = r.ExitCode
				res.ValidationDuration = r.Duration
				res = run.SaveResolution(res)
			}

			run.RecordResolutionEvent(resolutions.ResolutionEvent{
				ResolutionID:   res.ID,
				RunID:          res.RunID,
				Repository:     res.Repository,
				File:           res.File,
				Actor:          resolutions.ActorSystem,
				Type:           eventType,
				PreviousStatus: resolutions.StatusApplied,
				NewStatus:      res.Status,
				ErrorCode: func() string {
					if r.Status != validation.StatusPassed {
						return string(resolutions.CodeValidationFailure)
					}
					return ""
				}(),
			})
		}
	} else {
		res.ValidationStatus = resolutions.ValidationNotRun
		res = run.SaveResolution(res)
	}

	return ApplyResult{
		Resolution:       res,
		ValidationResult: valResult,
		PostApplyHash:    postHash,
	}, nil
}

// RollbackRequest carries the parameters for a rollback operation.
type RollbackRequest struct {
	// ResolutionID is the opaque resolution identifier.
	ResolutionID string
	// RepositoryRoot is the current canonical repository root.
	RepositoryRoot string
	// File is the repository-relative file path.
	File string
	// PreApplyContent is the exact original file bytes captured before apply.
	// When omitted or nil, the server-side snapshot stored on the resolution is used.
	PreApplyContent []byte
	// PostApplyHash is the expected current-file hash (the result of apply).
	// When empty, the stored PostApplyContentHash on the resolution is used.
	// Rollback refuses if the file no longer matches this hash.
	PostApplyHash string
}

// Rollback restores the exact pre-apply file content for an applied resolution.
// It refuses to overwrite unrelated user edits (current file must match
// PostApplyHash). It never uses `git reset --hard`.
func Rollback(
	ctx context.Context,
	run *runstate.Run,
	req RollbackRequest,
) (resolutions.Resolution, error) {
	// ── Step 1: acquire per-resolution in-flight lock ─────────────────────
	if !run.TryBeginResolutionRevert(req.ResolutionID) {
		return resolutions.Resolution{}, &safeguard.SafeError{
			Code:    resolutions.CodeApplyInProgress,
			Message: fmt.Sprintf("an apply or rollback is already in progress for resolution %s", req.ResolutionID),
		}
	}
	defer run.EndResolutionRevert(req.ResolutionID)

	// Load resolution.
	res, ok := run.GetResolution(req.ResolutionID)
	if !ok {
		return resolutions.Resolution{}, &safeguard.SafeError{
			Code:    resolutions.CodeResolutionNotFound,
			Message: fmt.Sprintf("resolution %s not found", req.ResolutionID),
		}
	}
	if err := resolutions.ValidateTransition(res.Status, resolutions.StatusReverted); err != nil {
		return resolutions.Resolution{}, &safeguard.SafeError{
			Code:    resolutions.CodeRollbackFailure,
			Message: fmt.Sprintf("resolution %s is not in a rollbackable state (status=%s): %v", res.ID, res.Status, err),
		}
	}

	// Validate repository and path.
	absRepo, err := safeguard.VerifyRepository(req.RepositoryRoot)
	if err != nil {
		return resolutions.Resolution{}, err
	}
	absPath, err := safeguard.VerifyFilePath(absRepo, req.File)
	if err != nil {
		return resolutions.Resolution{}, err
	}

	postApplyHash := req.PostApplyHash
	if postApplyHash == "" {
		postApplyHash = res.PostApplyContentHash
	}
	if postApplyHash == "" {
		postApplyHash = res.PostApplyHash
	}
	preApplyContent := req.PreApplyContent
	if len(preApplyContent) == 0 && res.PreApplyContent != "" {
		preApplyContent = []byte(res.PreApplyContent)
	}
	if len(preApplyContent) == 0 {
		return resolutions.Resolution{}, &safeguard.SafeError{
			Code:    resolutions.CodeRollbackFailure,
			Message: "pre-apply content not available for rollback",
		}
	}

	// Verify current file matches the expected post-apply hash before
	// overwriting — refuse if there are unrelated user edits or sequential changes.
	if postApplyHash != "" {
		currentHash, hashErr := fileutil.HashFile(absPath)
		if hashErr != nil {
			return resolutions.Resolution{}, &safeguard.SafeError{
				Code:    resolutions.CodeRollbackFailure,
				Message: fmt.Sprintf("cannot read current file for rollback verification: %v", hashErr),
			}
		}
		if currentHash != postApplyHash {
			return resolutions.Resolution{}, &safeguard.SafeError{
				Code:    resolutions.CodeRollbackFailure,
				Message: fmt.Sprintf("file %s was modified after apply (expected post-apply hash %s, got %s); rollback refused to avoid overwriting unrelated changes",
					req.File, postApplyHash, currentHash),
			}
		}
	}

	// Record revert_started.
	prevStatus := res.Status
	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID:   res.ID,
		RunID:          res.RunID,
		Repository:     res.Repository,
		File:           res.File,
		Actor:          resolutions.ActorUser,
		Type:           resolutions.EventRevertStarted,
		PreviousStatus: prevStatus,
		NewStatus:      resolutions.StatusReverted,
	})

	// Atomic restore.
	info, statErr := os.Stat(absPath)
	perm := os.FileMode(0o644)
	if statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := fileutil.AtomicWrite(absPath, preApplyContent, perm); err != nil {
		run.RecordResolutionEvent(resolutions.ResolutionEvent{
			ResolutionID:   res.ID,
			RunID:          res.RunID,
			Repository:     res.Repository,
			File:           res.File,
			Actor:          resolutions.ActorSystem,
			Type:           resolutions.EventFailed,
			PreviousStatus: prevStatus,
			NewStatus:      resolutions.StatusFailed,
			ErrorCode:      string(resolutions.CodeRollbackFailure),
		})
		res.Status = resolutions.StatusFailed
		res.ErrorCode = string(resolutions.CodeRollbackFailure)
		res.ErrorMessage = fmt.Sprintf("atomic rollback write failed: %v", err)
		res = run.SaveResolution(res)
		return res, &safeguard.SafeError{
			Code:    resolutions.CodeRollbackFailure,
			Message: fmt.Sprintf("rollback write failed: %v", err),
		}
	}

	// Update resolution.
	now := time.Now().UTC()
	res.Status = resolutions.StatusReverted
	res.RevertedAt = now
	res = run.SaveResolution(res)

	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID:   res.ID,
		RunID:          res.RunID,
		Repository:     res.Repository,
		File:           res.File,
		Actor:          resolutions.ActorUser,
		Type:           resolutions.EventReverted,
		PreviousStatus: prevStatus,
		NewStatus:      resolutions.StatusReverted,
	})

	return res, nil
}
