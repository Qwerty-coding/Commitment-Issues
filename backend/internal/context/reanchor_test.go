package promptcontext

import (
	"strconv"
	"strings"
	"testing"

	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
)

// buildAnalysis constructs a FileAnalysis for content exactly as the engine
// does: parse regions, hash content, hash per-region context.
func buildAnalysis(t *testing.T, content string) FileAnalysis {
	t.Helper()
	parsed := git.ParseConflictRegions(content)
	if len(parsed.Regions) == 0 {
		t.Fatalf("test fixture has no conflict regions")
	}
	return FileAnalysis{
		File:                "f.go",
		ContentHash:         fileutil.HashContent([]byte(content)),
		ConflictRegions:     parsed.Regions,
		RegionContextHashes: regionHashesFor([]byte(content), parsed.Regions),
	}
}

const threeRegionFile = "package main\n\n" +
	"func a() {\n" +
	"<<<<<<< HEAD\n" +
	"\treturn 1\n" +
	"=======\n" +
	"\treturn 2\n" +
	">>>>>>> feature\n" +
	"}\n\n" +
	"func b() {\n" +
	"<<<<<<< HEAD\n" +
	"\treturn 3\n" +
	"=======\n" +
	"\treturn 4\n" +
	">>>>>>> feature\n" +
	"}\n\n" +
	"func c() {\n" +
	"<<<<<<< HEAD\n" +
	"\treturn 5\n" +
	"=======\n" +
	"\treturn 6\n" +
	">>>>>>> feature\n" +
	"}\n"

// TestReanchorAnalysis_PreservesOtherRegions verifies that after resolving
// region 0, the analysis for regions 1 and 2 is refreshed (new content hash,
// shifted line numbers, fresh context hashes) so their suggestions stay valid.
func TestReanchorAnalysis_PreservesOtherRegions(t *testing.T) {
	prev := buildAnalysis(t, threeRegionFile)
	if len(prev.ConflictRegions) != 3 {
		t.Fatalf("expected 3 regions, got %d", len(prev.ConflictRegions))
	}

	// Apply region 0: replace its 5 marker lines with a single merged line.
	lines := strings.Split(strings.TrimSuffix(threeRegionFile, "\n"), "\n")
	region := prev.ConflictRegions[0]
	merged := append([]string{}, lines[:region.StartLine-1]...)
	merged = append(merged, "\treturn 1 + 2")
	merged = append(merged, lines[region.EndLine:]...)
	newContent := strings.Join(merged, "\n") + "\n"

	result := ReanchorAnalysis(prev, []byte(newContent))
	if !result.OK {
		t.Fatalf("expected re-anchor to succeed")
	}

	// Region 0 is gone; regions 1 and 2 map to new indices 0 and 1.
	if len(result.Analysis.ConflictRegions) != 2 {
		t.Fatalf("expected 2 remaining regions, got %d", len(result.Analysis.ConflictRegions))
	}
	if got := result.MappedRegionIDs["1"]; got != "0" {
		t.Errorf("old region 1 should map to new 0, got %q", got)
	}
	if got := result.MappedRegionIDs["2"]; got != "1" {
		t.Errorf("old region 2 should map to new 1, got %q", got)
	}
	if _, applied := result.MappedRegionIDs["0"]; applied {
		t.Errorf("applied region 0 should not be mapped")
	}

	// The refreshed snapshot must now validate the NEW working-tree content:
	// this is the whole point — the other regions are no longer "stale".
	if result.Analysis.ContentHash != fileutil.HashContent([]byte(newContent)) {
		t.Errorf("content hash not refreshed")
	}

	// Simulate the next region's preview: verify its context hash matches the
	// stored (refreshed) hash computed against the new content.
	allLines := strings.Split(strings.TrimSuffix(newContent, "\n"), "\n")
	for _, r := range result.Analysis.ConflictRegions {
		want := result.Analysis.RegionContextHashes[strconv.Itoa(r.Index)]
		got := fileutil.RegionContextHash(allLines, r)
		if got != want {
			t.Errorf("region %d context hash mismatch after re-anchor", r.Index)
		}
	}
}

// TestReanchorAnalysis_AmbiguousFails verifies that when the mapping cannot be
// trusted (two regions become byte-identical, so a single old region matches
// two new ones), OK is false so the caller falls back to invalidation.
func TestReanchorAnalysis_AmbiguousFails(t *testing.T) {
	// Two regions with identical content would both match one remaining region.
	content := "a\n" +
		"<<<<<<< HEAD\n" +
		"\tx\n" +
		"=======\n" +
		"\ty\n" +
		">>>>>>> f\n" +
		"<<<<<<< HEAD\n" +
		"\tx\n" +
		"=======\n" +
		"\ty\n" +
		">>>>>>> f\n"
	prev := buildAnalysis(t, content)
	if len(prev.ConflictRegions) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(prev.ConflictRegions))
	}

	// Remove one region entirely, leaving a single region identical to both.
	oneRegion := "a\n" +
		"<<<<<<< HEAD\n" +
		"\tx\n" +
		"=======\n" +
		"\ty\n" +
		">>>>>>> f\n"
	result := ReanchorAnalysis(prev, []byte(oneRegion))
	if result.OK {
		t.Fatalf("ambiguous mapping must not be trusted (OK should be false)")
	}
}
