package promptcontext

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// readmeCandidates is the deterministic discovery order for repository READMEs.
// First match wins; the search is case-sensitive on case-sensitive file systems.
var readmeCandidates = []string{
	"README.md",
	"readme.md",
	"README.markdown",
	"README.txt",
	"README",
}

// LoadReadmeContext discovers the repository README, normalizes CRLF to LF,
// and truncates at a line boundary so the excerpt never exceeds maxBytes.
// Missing README is not an error — callers receive empty strings and nil.
//
// The function is deterministic: given the same repoRoot and maxBytes it always
// returns the same excerpt. No subdirectory READMEs are considered.
func LoadReadmeContext(repoRoot string, maxBytes int) (excerpt, source string, err error) {
	if maxBytes <= 0 {
		return "", "", nil
	}

	for _, candidate := range readmeCandidates {
		path := filepath.Join(repoRoot, candidate)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return "", "", readErr
		}

		// Normalize CRLF → LF for consistent byte counting and rendering.
		normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

		text := string(normalized)
		if len(text) <= maxBytes {
			return text, candidate, nil
		}

		// Truncate at the last newline boundary within maxBytes so we never
		// cut in the middle of a line.
		cut := text[:maxBytes]
		if idx := strings.LastIndexByte(cut, '\n'); idx >= 0 {
			cut = cut[:idx+1]
		}
		return cut + "… (truncated)", candidate, nil
	}

	return "", "", nil
}
