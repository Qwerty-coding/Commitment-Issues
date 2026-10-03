# Phase 3 Plan: Controlled Resolution Application

## Objective

Turn validated AI suggestions into safe, reviewable repository changes. Phase 3
must never apply code automatically: every mutation requires explicit user
approval and must be reversible.

## Priority Order

1. Patch model and preview
2. Stale-file and workspace safety
3. Explicit approval workflow
4. Apply with rollback
5. Post-apply validation
6. Audit history and frontend workflow

## 1. Patch model and preview

Create a patch/resolution model linked to the existing run-scoped suggestion:

- Stable suggestion ID
- Run ID
- Repository root
- Relative file path
- Conflict identity and region/range
- Original file content hash
- Base, ours, and theirs content
- Proposed replacement content or unified patch
- Validation status
- Approval status
- Created, approved, applied, and reverted timestamps
- Error code and redacted error message

Preview must be read-only. It must show the exact affected region and the
resulting file diff without writing to disk or changing the Git index.

## 2. Safety checks before approval and apply

Before applying a patch:

- Confirm the repository is still the expected repository.
- Confirm the relative path stays inside that repository.
- Re-read the file and compare its hash with the analysis-time hash.
- Confirm the conflict region and surrounding context still match.
- Reject stale or ambiguous patches rather than guessing.
- Refuse to write files outside the repository root.
- Refuse to apply a suggestion whose source file or collision identity no longer exists.
- Never execute AI-provided commands or scripts.

The user must be shown a stale-file error and must re-analyze before retrying.

## 3. API design

Add additive, repository-qualified endpoints. Preserve existing Phase 2
response fields.

- `GET /api/resolutions/{id}`: retrieve one suggestion and patch state.
- `POST /api/resolutions/{id}/preview`: generate a read-only preview.
- `POST /api/resolutions/{id}/approve`: record explicit approval; do not write files.
- `POST /api/resolutions/{id}/apply`: apply one approved patch.
- `POST /api/resolutions/{id}/revert`: revert a previously applied patch.
- `GET /api/resolutions/{id}/validation`: retrieve formatter/compiler/test results.

Every mutation endpoint must require the stable suggestion ID and an explicit
repository/file association. Do not support ambiguous bare-file mutations.

Use stable typed errors for:

- Not found
- Not approved
- Already applied
- Stale file
- Invalid patch
- Path escape
- Repository state mismatch
- Validation failure
- Rollback failure

## 4. Approval and apply workflow

The only valid mutation sequence is:

1. Generate and validate a suggestion.
2. Preview the proposed patch.
3. User explicitly approves that suggestion.
4. Revalidate the file hash and conflict context.
5. Create a backup or reversible snapshot.
6. Apply the patch atomically.
7. Record the applied state and audit event.
8. Run post-apply validation.

Approval must expire when the source file changes or when the suggestion is
regenerated.

Applying one suggestion must not silently apply other suggestions in the same
file or repository.

## 5. Rollback

Rollback must:

- Be available only for patches applied by the current tool.
- Verify the current file still matches the expected post-apply hash.
- Refuse to overwrite unrelated user changes.
- Restore the exact pre-apply content atomically.
- Record success or failure in audit history.
- Preserve the original suggestion, preview, and validation results.

Do not use destructive Git commands such as `git reset --hard`.

## 6. Post-apply validation

After applying a patch, run only explicitly configured repository validation
commands. The initial implementation should support safe, known commands such
as language formatters and test/build commands configured by the user.

- Do not infer shell commands from AI output.
- Capture exit code, stdout/stderr with size limits, duration, and cancellation.
- Mark the resolution as validation-passed, validation-failed, or not-run.
- A validation failure must not automatically revert changes without approval.
- The UI must distinguish “applied but validation failed” from “apply failed.”

## 7. Run-scoped audit state

Extend run-scoped state with:

- Resolution lifecycle status: proposed, previewed, approved, applied, reverted,
  stale, failed, validation_failed.
- Immutable event records for preview, approval, apply, validation, and revert.
- Actor/source information such as user approval or CLI invocation.
- Repository-qualified keys and deterministic ordering.

Keep the current bounded in-memory behavior initially. Durable persistence is a
separate phase unless explicitly approved.

## 8. Frontend workflow

Update Suggestions with:

- Read-only patch preview.
- Explicit Approve button.
- Apply button enabled only after approval.
- Stale-file warning and re-analysis action.
- Apply progress and result states.
- Validation result display.
- Revert action with confirmation.
- Clear distinction between AI confidence and validation status.

Do not add automatic apply, automatic approval, or automatic rollback.

## Tests

### Backend

- Patch generation for one conflict region.
- Multiple conflict regions in one file.
- Stale file hash rejection.
- Changed surrounding context rejection.
- Path traversal rejection.
- Repository mismatch rejection.
- Approval required before apply.
- Approval invalidated after regeneration.
- Atomic apply success.
- Apply failure without partial file corruption.
- Rollback success.
- Rollback refusal after unrelated edits.
- Duplicate apply rejection.
- Validation success, failure, timeout, and cancellation.
- Audit event ordering and run/repository isolation.
- Concurrent apply attempts for the same suggestion.
- No AI-provided command execution.

### Frontend

Verify manually or with browser tests:

- Preview does not modify the file.
- Approval is explicit.
- Apply is disabled before approval.
- Stale-file state is understandable.
- Validation failures are visible.
- Revert requires confirmation.
- Refresh/navigation preserves the selected suggestion.

## Verification

```text
cd backend
go test ./...
go test -race ./...
go vet ./...

cd ../frontend
npm run lint
npm run build
```

Also manually test against a temporary Git repository with real conflicts,
uncommitted unrelated edits, multiple files, and interrupted validation.

## Out of Scope

- Autonomous conflict resolution.
- Automatic approval or commits.
- `git reset --hard` or destructive workspace cleanup.
- Applying multiple suggestions without individual approval.
- Database or distributed persistence.
- Broad dashboard redesign.
- New language parsers.
- General server security/lifecycle redesign unless required to protect apply.