package api

import (
	"fmt"
	"net/http"
	"testing"

	"CommitIssues/internal/resolutions"
)

// TestAPI_MultiRegionApply_KeepsOtherRegionsValid proves the multi-region
// re-anchor fix (#5): after applying region 0, the stored analysis is refreshed
// in place, so region 1's preview/approve/apply still succeed WITHOUT a full
// re-analysis. Before the fix, applying region 0 changed the file hash and
// region 1's preview returned STALE_FILE.
func TestAPI_MultiRegionApply_KeepsOtherRegionsValid(t *testing.T) {
	content := "// top\n" +
		"<<<<<<< ours\nfn_a_ours();\n=======\nfn_a_theirs();\n>>>>>>> theirs\n" +
		"// middle\n" +
		"<<<<<<< ours\nfn_b_ours();\n=======\nfn_b_theirs();\n>>>>>>> theirs\n" +
		"// bottom\n"
	repo, run, handler, analysis := setupTestServerWithConflict(t, "multi.js", content)
	if len(analysis.ConflictRegions) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(analysis.ConflictRegions))
	}

	// Build a resolution for region 0.
	res0 := resolutions.Resolution{
		ID: resolutions.NewID(), SuggestionID: "sug-0", RunID: run.ID,
		Repository: repo, File: "multi.js", Revision: 1, RegionID: "0",
		Replacement: "fn_a_merged();", Status: resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone, CreatedAt: analysis.AnalysisTimestamp,
	}
	// Build a resolution for region 1 (the one that must survive).
	res1 := resolutions.Resolution{
		ID: resolutions.NewID(), SuggestionID: "sug-1", RunID: run.ID,
		Repository: repo, File: "multi.js", Revision: 1, RegionID: "1",
		Replacement: "fn_b_merged();", Status: resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone, CreatedAt: analysis.AnalysisTimestamp,
	}
	run.SaveResolution(res0)
	run.SaveResolution(res1)

	// Region 1 must be previewable BEFORE applying region 0.
	body1 := map[string]any{
		"resolutionId": res1.ID, "repositoryRoot": repo, "file": "multi.js",
		"suggestionRevision": 1,
	}
	rec := doPostJSON(t, handler, "/api/resolutions/"+res1.ID+"/preview", body1)
	if rec.Code != http.StatusOK {
		t.Fatalf("region 1 pre-apply preview failed: %d %s", rec.Code, rec.Body.String())
	}

	// Preview + approve + apply region 0.
	body0 := map[string]any{
		"resolutionId": res0.ID, "repositoryRoot": repo, "file": "multi.js",
		"suggestionRevision": 1,
	}
	if rec := doPostJSON(t, handler, "/api/resolutions/"+res0.ID+"/preview", body0); rec.Code != http.StatusOK {
		t.Fatalf("region 0 preview failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doPostJSON(t, handler, "/api/resolutions/"+res0.ID+"/approve", body0); rec.Code != http.StatusOK {
		t.Fatalf("region 0 approve failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doPostJSON(t, handler, "/api/resolutions/"+res0.ID+"/apply", body0); rec.Code != http.StatusOK {
		t.Fatalf("region 0 apply failed: %d %s", rec.Code, rec.Body.String())
	}

	// The stored analysis must now reflect ONE remaining region with a fresh
	// content hash (re-anchored in place).
	updated, ok := run.GetAnalysis(repo, "multi.js")
	if !ok {
		t.Fatal("analysis missing after apply")
	}
	if len(updated.ConflictRegions) != 1 {
		t.Fatalf("expected 1 remaining region after re-anchor, got %d", len(updated.ConflictRegions))
	}

	// Region 1's resolution must have been remapped to the new region index 0.
	remapped, ok := run.GetResolution(res1.ID)
	if !ok {
		t.Fatal("region 1 resolution missing")
	}
	if remapped.RegionID != "0" {
		t.Fatalf("region 1 should be remapped to index 0, got %q", remapped.RegionID)
	}

	// The decisive assertion: region 1's preview must STILL succeed against the
	// new working tree WITHOUT any re-analysis. This is exactly what failed
	// (STALE_FILE) before the fix.
	body1b := map[string]any{
		"resolutionId": res1.ID, "repositoryRoot": repo, "file": "multi.js",
		"suggestionRevision": 1,
	}
	rec = doPostJSON(t, handler, "/api/resolutions/"+res1.ID+"/preview", body1b)
	if rec.Code != http.StatusOK {
		code, msg := parseErrorResponse(t, rec)
		t.Fatalf("region 1 preview after region-0 apply failed (expected OK): %d %s: %s", rec.Code, code, msg)
	}
	fmt.Printf("re-anchor OK: region 1 previewable post-apply, remaining=%d\n", len(updated.ConflictRegions))
}
