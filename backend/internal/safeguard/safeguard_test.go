package safeguard

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

func TestVerifyRepository_Success(t *testing.T) {
	tmp := t.TempDir()
	canon, err := VerifyRepository(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canon == "" {
		t.Error("canonical path must not be empty")
	}
}

func TestVerifyRepository_NonExistent(t *testing.T) {
	_, err := VerifyRepository("/non/existent/path/for/sure")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
	se, ok := err.(*SafeError)
	if !ok || se.Code != resolutions.CodeRepositoryMismatch {
		t.Errorf("expected CodeRepositoryMismatch, got %v", err)
	}
}

func TestVerifyRepositoryIdentity_Mismatch(t *testing.T) {
	repo1 := t.TempDir()
	repo2 := t.TempDir()

	err := VerifyRepositoryIdentity(repo1, repo2)
	if err == nil {
		t.Fatal("expected error for different repositories")
	}
	se, ok := err.(*SafeError)
	if !ok || se.Code != resolutions.CodeRepositoryMismatch {
		t.Errorf("expected CodeRepositoryMismatch, got %v", err)
	}
}

func TestVerifyFilePath_Safe(t *testing.T) {
	tmp := t.TempDir()
	abs, err := VerifyFilePath(tmp, "src/components/Button.jsx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(tmp, "src", "components", "Button.jsx")
	if abs != want {
		t.Errorf("got %s, want %s", abs, want)
	}
}

func TestVerifyFilePath_PathTraversal(t *testing.T) {
	tmp := t.TempDir()
	traversals := []string{
		"../secret.txt",
		"foo/../../etc/passwd",
		"..",
		"a/b/../../../c",
	}
	for _, p := range traversals {
		t.Run(p, func(t *testing.T) {
			_, err := VerifyFilePath(tmp, p)
			if err == nil {
				t.Fatalf("expected error for path traversal %q", p)
			}
			se, ok := err.(*SafeError)
			if !ok || se.Code != resolutions.CodePathEscape {
				t.Errorf("expected CodePathEscape for %q, got %v", p, err)
			}
		})
	}
}

func TestVerifyFilePath_WindowsDriveEscape(t *testing.T) {
	tmp := t.TempDir()
	escapes := []string{
		"C:escaped.txt",
		"D:/foo/bar",
		"c:\\windows\\system32",
	}
	for _, p := range escapes {
		t.Run(p, func(t *testing.T) {
			_, err := VerifyFilePath(tmp, p)
			if err == nil {
				t.Fatalf("expected error for windows drive escape %q", p)
			}
			se, ok := err.(*SafeError)
			if !ok || se.Code != resolutions.CodePathEscape {
				t.Errorf("expected CodePathEscape for %q, got %v", p, err)
			}
		})
	}
}

func TestVerifyFilePath_AbsoluteReject(t *testing.T) {
	tmp := t.TempDir()
	absPath := filepath.Join(tmp, "sub", "file.js")
	_, err := VerifyFilePath(tmp, absPath)
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
	se, ok := err.(*SafeError)
	if !ok || se.Code != resolutions.CodePathEscape {
		t.Errorf("expected CodePathEscape, got %v", err)
	}
}

func TestVerifyRevision(t *testing.T) {
	if err := VerifyRevision(1, 1); err != nil {
		t.Errorf("matching revision should succeed: %v", err)
	}

	err := VerifyRevision(1, 2)
	if err == nil {
		t.Fatal("mismatched revision should fail")
	}
	se, ok := err.(*SafeError)
	if !ok || se.Code != resolutions.CodeApprovalExpired {
		t.Errorf("expected CodeApprovalExpired, got %v", err)
	}
}

func TestVerifyApproval(t *testing.T) {
	resApproved := resolutions.Resolution{ApprovalStatus: resolutions.ApprovalApproved}
	if err := VerifyApproval(resApproved); err != nil {
		t.Errorf("approved should succeed: %v", err)
	}

	resNone := resolutions.Resolution{ID: "res-none", ApprovalStatus: resolutions.ApprovalNone}
	err := VerifyApproval(resNone)
	if err == nil {
		t.Fatal("none approval should fail")
	}
	se, ok := err.(*SafeError)
	if !ok || se.Code != resolutions.CodeNotApproved {
		t.Errorf("expected CodeNotApproved, got %v", err)
	}

	resExpired := resolutions.Resolution{ID: "res-exp", ApprovalStatus: resolutions.ApprovalExpired}
	err = VerifyApproval(resExpired)
	if err == nil {
		t.Fatal("expired approval should fail")
	}
	se, ok = err.(*SafeError)
	if !ok || se.Code != resolutions.CodeApprovalExpired {
		t.Errorf("expected CodeApprovalExpired, got %v", err)
	}
}

func TestVerifyStatus(t *testing.T) {
	if err := VerifyStatus(resolutions.Resolution{Status: resolutions.StatusProposed}); err != nil {
		t.Errorf("proposed should pass: %v", err)
	}
	if err := VerifyStatus(resolutions.Resolution{Status: resolutions.StatusApproved}); err != nil {
		t.Errorf("approved should pass: %v", err)
	}

	applied := resolutions.Resolution{ID: "res-1", Status: resolutions.StatusApplied}
	err := VerifyStatus(applied)
	if err == nil || err.(*SafeError).Code != resolutions.CodeAlreadyApplied {
		t.Errorf("expected CodeAlreadyApplied, got %v", err)
	}

	stale := resolutions.Resolution{ID: "res-2", Status: resolutions.StatusStale}
	err = VerifyStatus(stale)
	if err == nil || err.(*SafeError).Code != resolutions.CodeStaleFile {
		t.Errorf("expected CodeStaleFile, got %v", err)
	}
}

func TestVerifyWorkingTree_StaleDetection(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "sample.js")
	content := "<<<<<<< ours\nfn_ours();\n=======\nfn_theirs();\n>>>>>>> theirs\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed := git.ParseConflictRegions(content)
	allLines := strings.Split(content, "\n")
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	ctxHashes := map[string]string{
		"0": fileutil.RegionContextHash(allLines, parsed.Regions[0]),
	}

	analysis := promptcontext.FileAnalysis{
		File:                "sample.js",
		ContentHash:         fileutil.HashContent([]byte(content)),
		ConflictRegions:     parsed.Regions,
		RegionContextHashes: ctxHashes,
	}

	// 1. Success case
	check, err := VerifyWorkingTree(filePath, analysis, "0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(check.Regions) != 1 {
		t.Errorf("expected 1 region, got %d", len(check.Regions))
	}

	// 2. File modified on disk (stale content hash)
	if err := os.WriteFile(filePath, []byte(content+"// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = VerifyWorkingTree(filePath, analysis, "0")
	if err == nil {
		t.Fatal("expected error on modified file")
	}
	se, ok := err.(*SafeError)
	if !ok || se.Code != resolutions.CodeStaleFile {
		t.Errorf("expected CodeStaleFile, got %v", err)
	}

	// 3. Conflict region disappeared (e.g. resolved externally)
	if err := os.WriteFile(filePath, []byte("fn_resolved();\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	noMarkerAnalysis := analysis
	noMarkerAnalysis.ContentHash = fileutil.HashContent([]byte("fn_resolved();\n"))
	_, err = VerifyWorkingTree(filePath, noMarkerAnalysis, "0")
	if err == nil {
		t.Fatal("expected error when conflict region missing")
	}
	se, ok = err.(*SafeError)
	if !ok || se.Code != resolutions.CodeStaleFile {
		t.Errorf("expected CodeStaleFile, got %v", err)
	}
}

func TestURLEncodedResolutionIDs(t *testing.T) {
	for i := 0; i < 50; i++ {
		id := resolutions.NewID()
		// Must not contain URL special characters
		if strings.ContainsAny(id, "/\\|:?#% &=") {
			t.Fatalf("ID %q contains URL unsafe character", id)
		}
		// Must round-trip identically in formatted string
		formatted := fmt.Sprintf("/api/resolutions/%s/preview", id)
		extracted := strings.TrimPrefix(formatted, "/api/resolutions/")
		extracted = strings.TrimSuffix(extracted, "/preview")
		if extracted != id {
			t.Errorf("expected %s, got %s", id, extracted)
		}
	}
}
