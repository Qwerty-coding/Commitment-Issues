package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Inline parsing: standard ────────────────────────────────────────────────

func TestParseConflictRegions_Standard(t *testing.T) {
	content := "line one\n<<<<<<< HEAD\nour change\n=======\ntheir change\n>>>>>>> feature\nline two\n"
	result := ParseConflictRegions(content)

	if len(result.Regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(result.Regions))
	}
	r := result.Regions[0]
	if r.Ours != "our change" {
		t.Errorf("ours = %q", r.Ours)
	}
	if r.Theirs != "their change" {
		t.Errorf("theirs = %q", r.Theirs)
	}
	if r.HasBase {
		t.Errorf("standard conflict should not have base")
	}
	if r.OursLabel != "HEAD" || r.TheirsLabel != "feature" {
		t.Errorf("labels = %q / %q", r.OursLabel, r.TheirsLabel)
	}
	if r.StartLine != 2 || r.EndLine != 6 {
		t.Errorf("lines = %d..%d, want 2..6", r.StartLine, r.EndLine)
	}
	if result.Malformed || result.Incomplete {
		t.Errorf("unexpected malformed/incomplete flags")
	}
	if result.OursText != "line one\nour change\nline two\n" {
		t.Errorf("OursText = %q", result.OursText)
	}
	if result.TheirText != "line one\ntheir change\nline two\n" {
		t.Errorf("TheirText = %q", result.TheirText)
	}
}

func TestParseConflictRegions_NoMarkers(t *testing.T) {
	result := ParseConflictRegions("just\nnormal\ncontent\n")
	if len(result.Regions) != 0 {
		t.Fatalf("expected no regions, got %d", len(result.Regions))
	}
	if result.Malformed || result.Incomplete {
		t.Errorf("clean file should not be malformed")
	}
	if result.OursText != "just\nnormal\ncontent\n" {
		t.Errorf("content should pass through unchanged, got %q", result.OursText)
	}
}

func TestParseConflictRegions_MultipleRegions(t *testing.T) {
	content := strings.Join([]string{
		"top",
		"<<<<<<< ours",
		"a1",
		"=======",
		"b1",
		">>>>>>> theirs",
		"middle",
		"<<<<<<< ours",
		"a2",
		"=======",
		"b2",
		">>>>>>> theirs",
		"bottom",
	}, "\n")

	result := ParseConflictRegions(content)
	if len(result.Regions) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(result.Regions))
	}
	if result.Regions[0].Ours != "a1" || result.Regions[1].Ours != "a2" {
		t.Errorf("regions not preserved: %#v", result.Regions)
	}
	if result.OursText != "top\na1\nmiddle\na2\nbottom" {
		t.Errorf("OursText = %q", result.OursText)
	}
	if result.TheirText != "top\nb1\nmiddle\nb2\nbottom" {
		t.Errorf("TheirText = %q", result.TheirText)
	}
}

func TestParseConflictRegions_AdjacentRegions(t *testing.T) {
	content := strings.Join([]string{
		"<<<<<<< ours",
		"a1",
		"=======",
		"b1",
		">>>>>>> theirs",
		"<<<<<<< ours",
		"a2",
		"=======",
		"b2",
		">>>>>>> theirs",
	}, "\n")

	result := ParseConflictRegions(content)
	if len(result.Regions) != 2 {
		t.Fatalf("expected 2 adjacent regions, got %d", len(result.Regions))
	}
	if result.Regions[0].EndLine != 5 || result.Regions[1].StartLine != 6 {
		t.Errorf("adjacent line ranges wrong: %d..%d then %d..%d",
			result.Regions[0].StartLine, result.Regions[0].EndLine,
			result.Regions[1].StartLine, result.Regions[1].EndLine)
	}
}

// ─── Inline parsing: diff3 ───────────────────────────────────────────────────

func TestParseConflictRegions_Diff3(t *testing.T) {
	content := strings.Join([]string{
		"<<<<<<< ours",
		"our line",
		"||||||| base",
		"base line",
		"=======",
		"their line",
		">>>>>>> theirs",
	}, "\n")

	result := ParseConflictRegions(content)
	if len(result.Regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(result.Regions))
	}
	r := result.Regions[0]
	if !r.HasBase {
		t.Fatal("diff3 region should have base")
	}
	if r.Base != "base line" {
		t.Errorf("base = %q", r.Base)
	}
	if r.BaseLabel != "base" {
		t.Errorf("base label = %q", r.BaseLabel)
	}
	if r.Ours != "our line" || r.Theirs != "their line" {
		t.Errorf("diff3 sides wrong: ours=%q theirs=%q", r.Ours, r.Theirs)
	}
}

func TestParseConflictRegions_Diff3EmptyBase(t *testing.T) {
	content := strings.Join([]string{
		"<<<<<<< ours",
		"our line",
		"||||||| base",
		"=======",
		"their line",
		">>>>>>> theirs",
	}, "\n")

	result := ParseConflictRegions(content)
	if len(result.Regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(result.Regions))
	}
	r := result.Regions[0]
	if !r.HasBase {
		t.Fatal("expected base section to be present")
	}
	if r.Base != "" {
		t.Errorf("expected empty base, got %q", r.Base)
	}
}

// ─── Inline parsing: malformed / incomplete ──────────────────────────────────

func TestParseConflictRegions_MissingTheirMarker(t *testing.T) {
	content := "<<<<<<< ours\nour line\n=======\ntheir line\n"
	result := ParseConflictRegions(content)

	if len(result.Regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(result.Regions))
	}
	if !result.Incomplete || !result.Regions[0].Incomplete {
		t.Errorf("region missing >>>>>>> should be flagged incomplete")
	}
	// Data must not be lost.
	if result.Regions[0].Ours != "our line" || result.Regions[0].Theirs != "their line" {
		t.Errorf("incomplete region lost data: %#v", result.Regions[0])
	}
}

func TestParseConflictRegions_MalformedStraySeparator(t *testing.T) {
	content := "before\n=======\nafter\n"
	result := ParseConflictRegions(content)
	if !result.Malformed {
		t.Errorf("stray ======= should be flagged malformed")
	}
	if len(result.Regions) != 0 {
		t.Errorf("stray separator should not create a region")
	}
	if result.OursText != "before\n=======\nafter\n" {
		t.Errorf("malformed content should be preserved, got %q", result.OursText)
	}
}

func TestParseConflictRegions_NestedMarkers(t *testing.T) {
	content := strings.Join([]string{
		"<<<<<<< ours",
		"<<<<<<< nested",
		"inner",
		"=======",
		"inner their",
		">>>>>>> nested theirs",
		"=======",
		"their line",
		">>>>>>> theirs",
	}, "\n")

	result := ParseConflictRegions(content)
	if !result.Malformed {
		t.Errorf("nested markers should be flagged malformed")
	}
	if len(result.Regions) != 1 {
		t.Fatalf("expected 1 outer region, got %d", len(result.Regions))
	}
	// The nested start marker text must be preserved somewhere.
	if !strings.Contains(result.Regions[0].Ours, "<<<<<<< nested") {
		t.Errorf("nested marker content lost: %q", result.Regions[0].Ours)
	}
}

// ─── Git integration ─────────────────────────────────────────────────────────

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	return dir
}

func mergeExpectingConflict(t *testing.T, dir, branch string) {
	t.Helper()
	cmd := exec.Command("git", "merge", "--no-edit", branch)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if err := cmd.Run(); err == nil {
		t.Fatalf("expected merge conflict with %s, but merge succeeded", branch)
	}
}

func TestExtractConflictVersions_StagedModifyModify(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "file.txt"), "base\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")

	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "file.txt"), "theirs\n")
	runGit(t, dir, "commit", "-am", "theirs")

	runGit(t, dir, "checkout", "main")
	writeFile(t, filepath.Join(dir, "file.txt"), "ours\n")
	runGit(t, dir, "commit", "-am", "ours")

	mergeExpectingConflict(t, dir, "feature")

	data, err := ExtractConflictVersions(context.Background(), dir, "file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !data.StageValidated {
		t.Errorf("expected stages to be validated")
	}
	if data.Source != SourceStaged {
		t.Errorf("source = %q, want staged", data.Source)
	}
	if strings.TrimSpace(data.OurVersion) != "ours" {
		t.Errorf("our version = %q", data.OurVersion)
	}
	if strings.TrimSpace(data.TheirVersion) != "theirs" {
		t.Errorf("their version = %q", data.TheirVersion)
	}
	if strings.TrimSpace(data.BaseVersion) != "base" {
		t.Errorf("base version = %q", data.BaseVersion)
	}
}

func TestExtractConflictVersions_MissingStage1_AddAdd(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "README.md"), "root\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "root")

	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "file.txt"), "theirs\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "theirs")

	runGit(t, dir, "checkout", "main")
	writeFile(t, filepath.Join(dir, "file.txt"), "ours\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "ours")

	mergeExpectingConflict(t, dir, "feature")

	_, err := ExtractConflictVersions(context.Background(), dir, "file.txt")
	if err == nil {
		t.Fatal("expected a MissingStageError for add/add conflict")
	}
	var missing *MissingStageError
	if !errors.As(err, &missing) {
		t.Fatalf("expected *MissingStageError, got %T: %v", err, err)
	}
	if missing.Stage != 1 {
		t.Errorf("missing stage = %d, want 1", missing.Stage)
	}
	if missing.File != "file.txt" {
		t.Errorf("error file = %q", missing.File)
	}
	if missing.Repository == "" {
		t.Errorf("error should carry the repository path")
	}
}

func TestExtractConflictVersions_MissingStage2_DeletedByUs(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "file.txt"), "base\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")

	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "file.txt"), "theirs\n")
	runGit(t, dir, "commit", "-am", "theirs")

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "rm", "file.txt")
	runGit(t, dir, "commit", "-m", "delete")

	mergeExpectingConflict(t, dir, "feature")

	_, err := ExtractConflictVersions(context.Background(), dir, "file.txt")
	if err == nil {
		t.Fatal("expected a MissingStageError when our side deleted the file")
	}
	var missing *MissingStageError
	if !errors.As(err, &missing) {
		t.Fatalf("expected *MissingStageError, got %T: %v", err, err)
	}
	if missing.Stage != 2 {
		t.Errorf("missing stage = %d, want 2 (ours)", missing.Stage)
	}
}

func TestExtractConflictVersions_InlineOnly(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "sample.txt"), "before\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nafter\n")

	data, err := ExtractConflictVersions(context.Background(), dir, "sample.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Source != SourceInline {
		t.Errorf("source = %q, want inline", data.Source)
	}
	if data.StageValidated {
		t.Errorf("inline conflicts must be exempt from stage validation")
	}
	if len(data.Regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(data.Regions))
	}
	if data.Regions[0].Ours != "ours" || data.Regions[0].Theirs != "theirs" {
		t.Errorf("region content wrong: %#v", data.Regions[0])
	}
}

func TestGetConflictedFiles_SortedAndFound(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "<<<<<<< ours\nx\n=======\ny\n>>>>>>> theirs\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "<<<<<<< ours\nx\n=======\ny\n>>>>>>> theirs\n")
	writeFile(t, filepath.Join(dir, "clean.txt"), "no markers here\n")

	files, err := GetConflictedFiles(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 conflicted files, got %d: %v", len(files), files)
	}
	if files[0] != "a.txt" || files[1] != "b.txt" {
		t.Errorf("files not sorted: %v", files)
	}
}

func TestExtractConflictVersions_ContextCancelled(t *testing.T) {
	dir := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ExtractConflictVersions(ctx, dir, "file.txt")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestFindGitRepositoryRoots_Cancelled(t *testing.T) {
	dir := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FindGitRepositoryRoots(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
