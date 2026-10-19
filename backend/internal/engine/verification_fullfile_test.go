package engine

import (
	"os"
	"path/filepath"
	"testing"

	git "CommitIssues/internal/git"
)

// TestProposedFileFor_BuildsFullFile proves the verification fix: a bare
// replacement snippet is spliced into the region to yield the FULL proposed
// file, so deterministic verification's syntax check parses real code instead
// of an isolated fragment (which would false-positive as a syntax error).
func TestProposedFileFor_BuildsFullFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	content := "package main\n\nfunc main() {\n<<<<<<< HEAD\n\tprintln(\"ours\")\n=======\n\tprintln(\"theirs\")\n>>>>>>> feature\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed := git.ParseConflictRegions(content)
	if len(parsed.Regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(parsed.Regions))
	}
	region := parsed.Regions[0]

	// A bare snippet that is NOT a valid standalone Go file.
	snippet := `println("merged")`
	full := proposedFileFor(path, region, snippet)

	// The full proposed file must be valid Go (the region markers replaced).
	if !containsStr(full, "package main") || containsStr(full, "<<<<<<<") {
		t.Fatalf("proposed file not correctly spliced:\n%s", full)
	}
	if !containsStr(full, `println("merged")`) {
		t.Fatalf("replacement missing from proposed file:\n%s", full)
	}
}

// TestProposedFileFor_FallsBackToSnippet verifies the safe fallback when the
// file cannot be read: the snippet is returned unchanged so verification still
// produces a verdict.
func TestProposedFileFor_FallsBackToSnippet(t *testing.T) {
	region := git.ConflictRegion{StartLine: 2, EndLine: 5, Ours: "a", Theirs: "b"}
	got := proposedFileFor(filepath.Join(t.TempDir(), "missing.go"), region, "replacement")
	if got != "replacement" {
		t.Fatalf("expected snippet fallback, got %q", got)
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
