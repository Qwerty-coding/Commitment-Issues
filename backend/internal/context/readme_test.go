package promptcontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper that creates a file with the given content inside dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

// ── Discovery order ────────────────────────────────────────────────────────────

func TestLoadReadmeContext_DiscoveryOrder_FirstMatch(t *testing.T) {
	dir := t.TempDir()
	// Write only README.md (the first candidate). On case-insensitive file
	// systems writing both README.md and readme.md would be the same file.
	writeFile(t, dir, "README.md", "# Primary\nHello from README.md\n")

	excerpt, source, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First candidate must win.
	if source != "README.md" {
		t.Errorf("source=%q, want %q", source, "README.md")
	}
	if !strings.Contains(excerpt, "Primary") {
		t.Errorf("excerpt does not contain 'Primary': %q", excerpt)
	}
}

func TestLoadReadmeContext_FallbackToReadmeMd_Lowercase(t *testing.T) {
	dir := t.TempDir()
	// On case-insensitive file systems (Windows, macOS default) "readme.md"
	// and "README.md" refer to the same file. The test is only meaningful on
	// a case-sensitive FS where the two names are distinct.
	//
	// Write "readme.md" and verify that it is found (via whichever candidate
	// matches first) and that the content is returned without error.
	writeFile(t, dir, "readme.md", "# Fallback\n")

	excerpt, _, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// On a case-insensitive FS the file is found as "README.md"; on a
	// case-sensitive FS it is found as "readme.md". Either way the content
	// must be present.
	if !strings.Contains(excerpt, "Fallback") {
		t.Errorf("expected Fallback in excerpt, got: %q", excerpt)
	}
}

func TestLoadReadmeContext_FallbackOrder_Markdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.markdown", "# Markdown\n")

	_, source, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source != "README.markdown" {
		t.Errorf("source=%q, want %q", source, "README.markdown")
	}
}

func TestLoadReadmeContext_FallbackOrder_Txt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.txt", "plain text readme\n")

	_, source, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source != "README.txt" {
		t.Errorf("source=%q, want %q", source, "README.txt")
	}
}

func TestLoadReadmeContext_FallbackOrder_Bare(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README", "bare readme\n")

	_, source, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source != "README" {
		t.Errorf("source=%q, want %q", source, "README")
	}
}


// ── Missing README ─────────────────────────────────────────────────────────────

func TestLoadReadmeContext_MissingReadme_NotAnError(t *testing.T) {
	dir := t.TempDir() // empty directory

	excerpt, source, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("expected nil error for missing README, got: %v", err)
	}
	if excerpt != "" {
		t.Errorf("expected empty excerpt, got: %q", excerpt)
	}
	if source != "" {
		t.Errorf("expected empty source, got: %q", source)
	}
}

// ── Truncation ─────────────────────────────────────────────────────────────────

func TestLoadReadmeContext_Truncation_AtLineBoundary(t *testing.T) {
	dir := t.TempDir()
	// Build content where the line boundary is unambiguous.
	// Line 1: "aaaa\n" (5 bytes), Line 2: "bbbb\n" (5 bytes), Line 3: "cccc\n" (5 bytes).
	// maxBytes=7 → the cut window is "aaaa\nbb" (7 bytes).
	// The last newline in that window is at index 4 (after "aaaa").
	// So the excerpt must be "aaaa\n" + truncation suffix — no "bbbb" or "cccc".
	content := "aaaa\nbbbb\ncccc\n"
	writeFile(t, dir, "README.md", content)

	excerpt, _, err := LoadReadmeContext(dir, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(excerpt, "… (truncated)") {
		t.Errorf("expected truncation suffix, got: %q", excerpt)
	}
	// The cut happens before "bbbb" starts.
	if strings.Contains(excerpt, "bbbb") {
		t.Errorf("excerpt contains 'bbbb' which should have been truncated: %q", excerpt)
	}
	if strings.Contains(excerpt, "cccc") {
		t.Errorf("excerpt contains 'cccc' which should have been truncated: %q", excerpt)
	}
}


func TestLoadReadmeContext_NoTruncation_WhenUnderLimit(t *testing.T) {
	dir := t.TempDir()
	content := "# Hello\nWorld\n"
	writeFile(t, dir, "README.md", content)

	excerpt, _, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(excerpt, "… (truncated)") {
		t.Errorf("unexpected truncation suffix for small content: %q", excerpt)
	}
	if excerpt != content {
		t.Errorf("excerpt=%q, want %q", excerpt, content)
	}
}

func TestLoadReadmeContext_Truncation_ExactBoundary(t *testing.T) {
	dir := t.TempDir()
	// "abc\n" is exactly 4 bytes. maxBytes=4 → no truncation needed.
	content := "abc\n"
	writeFile(t, dir, "README.md", content)

	excerpt, _, err := LoadReadmeContext(dir, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(excerpt, "… (truncated)") {
		t.Errorf("should not truncate at exact boundary: %q", excerpt)
	}
}

// ── CRLF normalization ─────────────────────────────────────────────────────────

func TestLoadReadmeContext_NormalizesCRLF(t *testing.T) {
	dir := t.TempDir()
	// Write Windows-style line endings.
	content := "# Hello\r\nWorld\r\n"
	writeFile(t, dir, "README.md", content)

	excerpt, _, err := LoadReadmeContext(dir, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(excerpt, "\r") {
		t.Errorf("excerpt contains CRLF after normalization: %q", excerpt)
	}
	if !strings.Contains(excerpt, "Hello") || !strings.Contains(excerpt, "World") {
		t.Errorf("content lost after CRLF normalization: %q", excerpt)
	}
}

func TestLoadReadmeContext_Truncation_CRLF_ByteCount(t *testing.T) {
	dir := t.TempDir()
	// After normalization "aa\nbb\n" is 6 bytes. maxBytes=3 cuts after "aa\n".
	content := "aa\r\nbb\r\n"
	writeFile(t, dir, "README.md", content)

	excerpt, _, err := LoadReadmeContext(dir, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(excerpt, "\r") {
		t.Errorf("CR present after normalization: %q", excerpt)
	}
	if strings.Contains(excerpt, "bb") {
		t.Errorf("truncation should have excluded 'bb': %q", excerpt)
	}
}

// ── Determinism ────────────────────────────────────────────────────────────────

func TestLoadReadmeContext_Determinism(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("Hello World\n", 200) // larger than 4096
	writeFile(t, dir, "README.md", content)

	first, _, _ := LoadReadmeContext(dir, 4096)
	for i := 0; i < 5; i++ {
		got, _, _ := LoadReadmeContext(dir, 4096)
		if got != first {
			t.Fatalf("non-deterministic output on iteration %d", i+1)
		}
	}
}

// ── Disabled config ────────────────────────────────────────────────────────────

func TestLoadReadmeContext_ZeroMaxBytes_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Should not be read\n")

	excerpt, source, err := LoadReadmeContext(dir, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excerpt != "" {
		t.Errorf("expected empty excerpt when maxBytes=0, got: %q", excerpt)
	}
	if source != "" {
		t.Errorf("expected empty source when maxBytes=0, got: %q", source)
	}
}

// ── Budget accounting (byte length) ───────────────────────────────────────────

func TestLoadReadmeContext_ExcerptNeverExceedsMaxBytes(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("X", 8192)
	writeFile(t, dir, "README.md", content)

	maxBytes := 4096
	excerpt, _, err := LoadReadmeContext(dir, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The raw text portion (before appending the suffix) must be ≤ maxBytes.
	// Strip the suffix for the raw byte check.
	raw := strings.TrimSuffix(excerpt, "… (truncated)")
	if len(raw) > maxBytes {
		t.Errorf("raw excerpt length %d exceeds maxBytes %d", len(raw), maxBytes)
	}
}

// ── Multi-repo isolation (runstate level) ─────────────────────────────────────

func TestRegisterReadme_MultiRepoIsolation(t *testing.T) {
	// Two repos with different READMEs must not bleed into each other.
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, dir1, "README.md", "# Repo One\n")
	writeFile(t, dir2, "README.md", "# Repo Two\n")

	e1, _, _ := LoadReadmeContext(dir1, 4096)
	e2, _, _ := LoadReadmeContext(dir2, 4096)

	if e1 == e2 {
		t.Errorf("two different READMEs produced identical excerpts: %q", e1)
	}
	if !strings.Contains(e1, "Repo One") {
		t.Errorf("repo1 excerpt missing 'Repo One': %q", e1)
	}
	if !strings.Contains(e2, "Repo Two") {
		t.Errorf("repo2 excerpt missing 'Repo Two': %q", e2)
	}
}
