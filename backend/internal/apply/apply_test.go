package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
	"CommitIssues/internal/resolutions"
	"CommitIssues/internal/runstate"
	"CommitIssues/internal/safeguard"
)

func createTestRepoWithConflict(t *testing.T, filename, content string) (string, promptcontext.FileAnalysis) {
	t.Helper()
	repo := t.TempDir()
	fullPath := filepath.Join(repo, filename)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	allLines := strings.Split(content, "\n")
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}

	parsed := git.ParseConflictRegions(content)
	ctxHashes := make(map[string]string)
	for _, r := range parsed.Regions {
		key := fmt.Sprintf("%d", r.Index)
		ctxHashes[key] = fileutil.RegionContextHash(allLines, r)
	}

	analysis := promptcontext.FileAnalysis{
		Repository:          repo,
		File:                filename,
		ContentHash:         fileutil.HashContent([]byte(content)),
		ConflictRegions:     parsed.Regions,
		RegionContextHashes: ctxHashes,
	}
	return repo, analysis
}

func TestApply_RejectedBeforeApproval(t *testing.T) {
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone, // Not approved!
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	req := ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 1,
	}

	_, err := Apply(context.Background(), run, analysis, req, nil)
	if err == nil {
		t.Fatal("expected apply to fail when not approved")
	}
	se, ok := err.(*safeguard.SafeError)
	if !ok || se.Code != resolutions.CodeNotApproved {
		t.Errorf("expected NOT_APPROVED code, got %v", err)
	}
}

func TestApply_ApprovalInvalidatedAfterRegeneration(t *testing.T) {
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	// Caller specifies regenerated revision 2 while resolution was for revision 1
	req := ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 2, // Mismatched revision!
	}

	_, err := Apply(context.Background(), run, analysis, req, nil)
	if err == nil {
		t.Fatal("expected apply to fail on mismatched revision")
	}
	se, ok := err.(*safeguard.SafeError)
	if !ok || se.Code != resolutions.CodeApprovalExpired {
		t.Errorf("expected APPROVAL_EXPIRED, got %v", err)
	}
}

func TestApply_SuccessfulAtomicApply(t *testing.T) {
	content := "// top\n<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n// bottom\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	req := ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 1,
	}

	result, err := Apply(context.Background(), run, analysis, req, nil)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if result.Resolution.Status != resolutions.StatusApplied {
		t.Errorf("expected StatusApplied, got %s", result.Resolution.Status)
	}

	// Verify file content on disk
	diskBytes, err := os.ReadFile(filepath.Join(repo, "file.js"))
	if err != nil {
		t.Fatal(err)
	}
	diskContent := string(diskBytes)
	if !strings.Contains(diskContent, "fn_resolved();") {
		t.Errorf("file missing resolved code: %s", diskContent)
	}
	if strings.Contains(diskContent, "<<<<<<<") {
		t.Errorf("file still contains conflict markers: %s", diskContent)
	}

	// Verify resulting post-apply hash matches actual file hash
	actualHash := fileutil.HashContent(diskBytes)
	if result.PostApplyHash != actualHash {
		t.Errorf("PostApplyHash %s != actualHash %s", result.PostApplyHash, actualHash)
	}

	// Verify audit events
	events := run.ResolutionEvents(res.ID)
	if len(events) < 2 {
		t.Errorf("expected at least 2 events (apply_started, applied), got %d", len(events))
	}
	if events[0].Type != resolutions.EventApplyStarted || events[len(events)-1].Type != resolutions.EventApplied {
		t.Errorf("unexpected event sequence: %+v", events)
	}
}

func TestApply_FailedApplyLeavesOriginalFileIntact(t *testing.T) {
	content := "// top\n<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "   \t  ", // Invalid replacement!
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	req := ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 1,
	}

	_, err := Apply(context.Background(), run, analysis, req, nil)
	if err == nil {
		t.Fatal("expected apply to fail on invalid replacement")
	}

	// Original file must be completely intact!
	diskBytes, _ := os.ReadFile(filepath.Join(repo, "file.js"))
	if string(diskBytes) != content {
		t.Errorf("original file was corrupted: got %s, want %s", string(diskBytes), content)
	}
}

func TestApply_DuplicateApplyProtection(t *testing.T) {
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	req := ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 1,
	}

	// First apply succeeds
	_, err := Apply(context.Background(), run, analysis, req, nil)
	if err != nil {
		t.Fatalf("first apply failed: %v", err)
	}

	// Second apply must be rejected with ALREADY_APPLIED
	_, err = Apply(context.Background(), run, analysis, req, nil)
	if err == nil {
		t.Fatal("second apply should be rejected")
	}
	se, ok := err.(*safeguard.SafeError)
	if !ok || se.Code != resolutions.CodeAlreadyApplied {
		t.Errorf("expected ALREADY_APPLIED, got %v", err)
	}
}

func TestApply_ConcurrentApplyProtection(t *testing.T) {
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	req := ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 1,
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, results[idx] = Apply(context.Background(), run, analysis, req, nil)
		}(i)
	}
	wg.Wait()

	// Exactly one must succeed, the other must fail with APPLY_IN_PROGRESS or ALREADY_APPLIED
	successCount := 0
	for _, rErr := range results {
		if rErr == nil {
			successCount++
		} else {
			se, ok := rErr.(*safeguard.SafeError)
			if !ok || (se.Code != resolutions.CodeApplyInProgress && se.Code != resolutions.CodeAlreadyApplied) {
				t.Errorf("unexpected concurrent error: %v", rErr)
			}
		}
	}
	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
}

func TestApply_DoesNotAffectOtherSuggestionsInSameFile(t *testing.T) {
	content := "// top\n" +
		"<<<<<<< ours\nfn0_ours();\n=======\nfn0_theirs();\n>>>>>>> theirs\n" +
		"// middle\n" +
		"<<<<<<< ours\nfn1_ours();\n=======\nfn1_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "multi.js", content)

	run := runstate.NewRun()
	res0 := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-0",
		RunID:          run.ID,
		Repository:     repo,
		File:           "multi.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn0_resolved();",
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	res1 := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "multi.js",
		Revision:       1,
		RegionID:       "1",
		Replacement:    "fn1_resolved();",
		Status:         resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res0)
	run.SaveResolution(res1)

	// Apply only res0
	req0 := ApplyRequest{
		ResolutionID:       res0.ID,
		RepositoryRoot:     repo,
		File:               "multi.js",
		SuggestionRevision: 1,
	}
	_, err := Apply(context.Background(), run, analysis, req0, nil)
	if err != nil {
		t.Fatalf("apply res0 failed: %v", err)
	}

	// Verify res1 remains completely untouched in runstate!
	storedRes1, _ := run.GetResolution(res1.ID)
	if storedRes1.Status != resolutions.StatusProposed || storedRes1.ApprovalStatus != resolutions.ApprovalNone {
		t.Errorf("res1 was mutated: status=%s, approval=%s", storedRes1.Status, storedRes1.ApprovalStatus)
	}

	// Verify region 1 markers remain on disk!
	diskBytes, _ := os.ReadFile(filepath.Join(repo, "multi.js"))
	diskContent := string(diskBytes)
	if !strings.Contains(diskContent, "fn0_resolved();") {
		t.Errorf("region 0 was not resolved")
	}
	if !strings.Contains(diskContent, "fn1_ours();") || !strings.Contains(diskContent, "fn1_theirs();") {
		t.Errorf("region 1 conflict was unexpectedly modified: %s", diskContent)
	}
}

func TestRollback_SuccessfulRollback(t *testing.T) {
	content := "// top\n<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	// 1. Apply
	applyRes, err := Apply(context.Background(), run, analysis, ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 1,
	}, nil)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	// 2. Rollback
	rollbackReq := RollbackRequest{
		ResolutionID:    res.ID,
		RepositoryRoot:  repo,
		File:            "file.js",
		PostApplyHash:   applyRes.PostApplyHash,
	}
	rolledBackRes, err := Rollback(context.Background(), run, rollbackReq)
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	if rolledBackRes.Status != resolutions.StatusReverted {
		t.Errorf("expected StatusReverted, got %s", rolledBackRes.Status)
	}

	// Verify disk file is restored to exact pre-apply content!
	diskBytes, _ := os.ReadFile(filepath.Join(repo, "file.js"))
	if string(diskBytes) != content {
		t.Errorf("disk content mismatch:\ngot: %s\nwant: %s", string(diskBytes), content)
	}

	// Verify audit events preserve all history
	events := run.ResolutionEvents(res.ID)
	hasReverted := false
	for _, ev := range events {
		if ev.Type == resolutions.EventReverted {
			hasReverted = true
			break
		}
	}
	if !hasReverted {
		t.Errorf("missing reverted event in audit: %+v", events)
	}
}

func TestRollback_RejectedAfterUnrelatedEdit(t *testing.T) {
	content := "// top\n<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	repo, analysis := createTestRepoWithConflict(t, "file.js", content)

	run := runstate.NewRun()
	res := resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "sug-1",
		RunID:          run.ID,
		Repository:     repo,
		File:           "file.js",
		Revision:       1,
		RegionID:       "0",
		Replacement:    "fn_resolved();",
		Status:         resolutions.StatusApproved,
		ApprovalStatus: resolutions.ApprovalApproved,
		CreatedAt:      time.Now().UTC(),
	}
	run.SaveResolution(res)

	// Apply
	applyRes, err := Apply(context.Background(), run, analysis, ApplyRequest{
		ResolutionID:       res.ID,
		RepositoryRoot:     repo,
		File:               "file.js",
		SuggestionRevision: 1,
	}, nil)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	// User makes an unrelated edit to the file after apply!
	diskPath := filepath.Join(repo, "file.js")
	appliedData, _ := os.ReadFile(diskPath)
	_ = os.WriteFile(diskPath, []byte(string(appliedData)+"\n// user edit"), 0o644)

	// Rollback must refuse to overwrite unrelated edit!
	rollbackReq := RollbackRequest{
		ResolutionID:   res.ID,
		RepositoryRoot: repo,
		File:           "file.js",
		PostApplyHash:  applyRes.PostApplyHash,
	}
	_, err = Rollback(context.Background(), run, rollbackReq)
	if err == nil {
		t.Fatal("expected rollback to be rejected after unrelated edit")
	}
	se, ok := err.(*safeguard.SafeError)
	if !ok || se.Code != resolutions.CodeRollbackFailure {
		t.Errorf("expected ROLLBACK_FAILURE, got %v", err)
	}
}
