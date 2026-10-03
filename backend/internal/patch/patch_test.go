package patch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
	"CommitIssues/internal/resolutions"
)

func sampleConflictFile(numRegions int) string {
	var sb strings.Builder
	sb.WriteString("// header line\n")
	sb.WriteString("const x = 1;\n\n")
	for i := 0; i < numRegions; i++ {
		sb.WriteString(fmt.Sprintf("// before region %d\n", i))
		sb.WriteString("<<<<<<< ours\n")
		sb.WriteString(fmt.Sprintf("function fn%d() { return 'ours_%d'; }\n", i, i))
		sb.WriteString("=======\n")
		sb.WriteString(fmt.Sprintf("function fn%d() { return 'theirs_%d'; }\n", i, i))
		sb.WriteString(">>>>>>> theirs\n")
		sb.WriteString(fmt.Sprintf("// after region %d\n\n", i))
	}
	sb.WriteString("export default x;\n")
	return sb.String()
}

func setupAnalysis(t *testing.T, content string) (promptcontext.FileAnalysis, []byte) {
	t.Helper()
	data := []byte(content)
	contentHash := fileutil.HashContent(data)

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
		Repository:          "/repo",
		File:                "sample.js",
		ContentHash:         contentHash,
		ConflictRegions:     parsed.Regions,
		RegionContextHashes: ctxHashes,
	}
	return analysis, data
}

func TestBuild_OneRegionPreview(t *testing.T) {
	content := sampleConflictFile(1)
	analysis, data := setupAnalysis(t, content)

	res := resolutions.Resolution{
		ID:          "res-1",
		File:        "sample.js",
		RegionID:    "0",
		Replacement: "function fn0() { return 'resolved_0'; }",
	}

	result, err := Build(analysis, res, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.ProposedContent, "resolved_0") {
		t.Errorf("proposed content missing replacement: %s", result.ProposedContent)
	}
	if strings.Contains(result.ProposedContent, "<<<<<<<") {
		t.Errorf("proposed content still contains markers: %s", result.ProposedContent)
	}
	if !strings.Contains(result.UnifiedDiff, "+function fn0() { return 'resolved_0'; }") {
		t.Errorf("unified diff missing added line: %s", result.UnifiedDiff)
	}
}

func TestBuild_MultipleRegionPreview_ModifiesOnlySelectedRegion(t *testing.T) {
	content := sampleConflictFile(3)
	analysis, data := setupAnalysis(t, content)

	// Replace only region 1
	res := resolutions.Resolution{
		ID:          "res-multi",
		File:        "sample.js",
		RegionID:    "1",
		Replacement: "function fn1() { return 'resolved_1'; }",
	}

	result, err := Build(analysis, res, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Region 1 should be replaced
	if !strings.Contains(result.ProposedContent, "resolved_1") {
		t.Errorf("region 1 was not replaced")
	}
	// Regions 0 and 2 must preserve their conflict markers untouched!
	if !strings.Contains(result.ProposedContent, "function fn0() { return 'ours_0'; }") {
		t.Errorf("region 0 ours content was modified")
	}
	if !strings.Contains(result.ProposedContent, "function fn2() { return 'theirs_2'; }") {
		t.Errorf("region 2 theirs content was modified")
	}
	countMarkers := strings.Count(result.ProposedContent, "<<<<<<<")
	if countMarkers != 2 {
		t.Errorf("expected exactly 2 remaining conflict regions, got %d", countMarkers)
	}
}

func TestBuild_PreviewDoesNotModifyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.js")
	content := sampleConflictFile(1)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	analysis, data := setupAnalysis(t, content)
	res := resolutions.Resolution{
		ID:          "res-1",
		File:        "sample.js",
		RegionID:    "0",
		Replacement: "function fn0() { return 'resolved_0'; }",
	}

	_, err := Build(analysis, res, data)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Read file from disk — must remain unmodified!
	after, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Errorf("preview modified on-disk file: got %s, want %s", string(after), content)
	}
}

func TestBuild_InvalidAIReplacement(t *testing.T) {
	content := sampleConflictFile(1)
	analysis, data := setupAnalysis(t, content)

	res := resolutions.Resolution{
		ID:          "res-1",
		File:        "sample.js",
		RegionID:    "0",
		Replacement: "   \n\t  ", // blank replacement
	}

	_, err := Build(analysis, res, data)
	if err == nil {
		t.Fatal("expected error for empty replacement, got nil")
	}
	pe, ok := err.(*PatchError)
	if !ok || pe.Code != resolutions.CodeInvalidPatch {
		t.Errorf("expected INVALID_PATCH error, got %v", err)
	}
}

func TestBuild_AmbiguousRegionRejection(t *testing.T) {
	content := sampleConflictFile(1)
	analysis, data := setupAnalysis(t, content)

	res := resolutions.Resolution{
		ID:          "res-1",
		File:        "sample.js",
		RegionID:    "999", // non-existent region
		Replacement: "valid replacement",
	}

	_, err := Build(analysis, res, data)
	if err == nil {
		t.Fatal("expected error for non-existent region, got nil")
	}
	pe, ok := err.(*PatchError)
	if !ok || pe.Code != resolutions.CodeInvalidPatch {
		t.Errorf("expected INVALID_PATCH error, got %v", err)
	}
}

func TestBuild_StaleFileHashRejection(t *testing.T) {
	content := sampleConflictFile(1)
	analysis, _ := setupAnalysis(t, content)

	// Modified file content
	modifiedData := []byte(content + "\n// user edit")

	res := resolutions.Resolution{
		ID:          "res-1",
		File:        "sample.js",
		RegionID:    "0",
		Replacement: "valid replacement",
	}

	_, err := Build(analysis, res, modifiedData)
	if err == nil {
		t.Fatal("expected STALE_FILE error, got nil")
	}
	pe, ok := err.(*PatchError)
	if !ok || pe.Code != resolutions.CodeStaleFile {
		t.Errorf("expected STALE_FILE error, got %v", err)
	}
}

func TestBuild_ChangedContextRejection(t *testing.T) {
	content := sampleConflictFile(1)
	analysis, data := setupAnalysis(t, content)

	// Tamper with the stored context hash to simulate changed context
	analysis.RegionContextHashes["0"] = "tampered-hash-value"

	res := resolutions.Resolution{
		ID:          "res-1",
		File:        "sample.js",
		RegionID:    "0",
		Replacement: "valid replacement",
	}

	_, err := Build(analysis, res, data)
	if err == nil {
		t.Fatal("expected STALE_FILE error due to changed context, got nil")
	}
	pe, ok := err.(*PatchError)
	if !ok || pe.Code != resolutions.CodeStaleFile {
		t.Errorf("expected STALE_FILE error, got %v", err)
	}
}
