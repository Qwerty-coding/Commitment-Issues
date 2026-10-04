// Package patch builds read-only unified diffs and validates proposed
// replacements for a single conflict region. It never writes to disk.
//
// Design constraints from the Phase 3 plan:
//
//   - Load the stored analysis snapshot, locate the exact conflict region,
//     verify it still matches the stored context, validate the replacement.
//   - Produce the proposed file content in memory and a read-only unified diff.
//   - Never write during preview.
//   - For multiple conflict regions: modify only the selected region, preserve
//     all others, never replace the entire file accidentally.
//   - If SuggestedCode cannot be safely mapped to the selected region, return
//     INVALID_PATCH — do not guess.
package patch

import (
	"fmt"
	"strings"

	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
	parser "CommitIssues/internal/parser"
	"CommitIssues/internal/resolutions"
)

// ErrInvalidPatch is the sentinel returned when a replacement cannot be
// safely mapped to the selected conflict region.
var ErrInvalidPatch = &PatchError{Code: resolutions.CodeInvalidPatch, Message: "replacement cannot be safely mapped to the stored conflict region"}

// ErrStaleFile is returned when the working-tree file changed since analysis.
var ErrStaleFile = &PatchError{Code: resolutions.CodeStaleFile, Message: "working-tree file changed since analysis; re-analyze before applying"}

// ErrAmbiguousRegion is returned when the region ID matches multiple or zero
// regions in the stored analysis.
var ErrAmbiguousRegion = &PatchError{Code: resolutions.CodeInvalidPatch, Message: "ambiguous or missing region match in stored analysis"}

// PatchError is a typed error carrying a stable error code.
type PatchError struct {
	Code    resolutions.ErrorCode
	Message string
}

func (e *PatchError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// PreviewResult is the outcome of a read-only patch build.
type PreviewResult struct {
	// ProposedContent is the full file content with the selected region
	// replaced by Replacement. It is built in memory; never written to disk
	// during preview.
	ProposedContent string
	// UnifiedDiff is the read-only unified diff between the original
	// working-tree content and ProposedContent.
	UnifiedDiff string
	// OriginalContent is the working-tree file content used as the base.
	OriginalContent string
	// RegionIndex is the 0-based index of the replaced conflict region.
	RegionIndex int
	// RequiresManualReview indicates the patch targets an unsupported language
	// or requires manual inspection.
	RequiresManualReview bool
}

// Build constructs a read-only unified diff and proposed file content for the
// selected conflict region of a resolution. It does NOT write to disk.
//
// Parameters:
//   - analysis:    the stored FileAnalysis snapshot from the analysis run.
//   - res:         the resolution containing the proposed replacement and
//     region identity (RegionID = fmt.Sprintf("%d", region.Index)).
//   - currentData: the current working-tree file bytes, already read by the
//     caller (who may have just verified the content hash).
//
// Returns ErrInvalidPatch if the replacement cannot be mapped, ErrStaleFile
// if the stored context hash no longer matches, or ErrAmbiguousRegion if the
// region cannot be uniquely located.
func Build(
	analysis promptcontext.FileAnalysis,
	res resolutions.Resolution,
	currentData []byte,
) (PreviewResult, error) {
	// 1. Verify the current content hash matches the stored analysis hash.
	currentHash := fileutil.HashContent(currentData)
	if analysis.ContentHash != "" && currentHash != analysis.ContentHash {
		return PreviewResult{}, ErrStaleFile
	}

	// 2. Locate the conflict region by its RegionID.
	region, err := findRegion(analysis.ConflictRegions, res.RegionID)
	if err != nil {
		return PreviewResult{}, err
	}

	// 3. Verify the stored context hash still matches the re-read content.
	if storedCtxHash, ok := analysis.RegionContextHashes[res.RegionID]; ok && storedCtxHash != "" {
		content := string(currentData)
		allLines := strings.Split(content, "\n")
		if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
			allLines = allLines[:len(allLines)-1]
		}
		currentCtxHash := fileutil.RegionContextHash(allLines, region)
		if currentCtxHash != storedCtxHash {
			return PreviewResult{}, ErrStaleFile
		}
	}

	// 4. Validate the replacement is non-empty (the caller validated syntax
	//    upstream; here we only reject obviously invalid cases).
	replacement := res.Replacement
	if strings.TrimSpace(replacement) == "" {
		return PreviewResult{}, ErrInvalidPatch
	}

	// 5. Reconstruct the proposed file content by replacing only the selected
	//    region's lines (StartLine..EndLine inclusive, 1-based).
	originalContent := string(currentData)
	proposed, err := applyRegionReplacement(originalContent, region, replacement)
	if err != nil {
		return PreviewResult{}, err
	}

	// 6. Verify all other conflict regions are preserved byte-for-byte.
	origLines := strings.Split(originalContent, "\n")
	for _, other := range analysis.ConflictRegions {
		if other.Index == region.Index {
			continue
		}
		if other.StartLine >= 1 && other.EndLine <= len(origLines) {
			otherLines := origLines[other.StartLine-1 : other.EndLine]
			otherText := strings.Join(otherLines, "\n")
			if !strings.Contains(proposed, otherText) {
				return PreviewResult{}, &PatchError{
					Code:    resolutions.CodeInvalidPatch,
					Message: fmt.Sprintf("patch modified or corrupted other conflict region %d", other.Index),
				}
			}
		}
	}

	// 7. Parse proposed result for supported languages. Unsupported languages
	//    are marked as requiring manual review.
	isSupported := parser.IsSupportedLanguage(res.File)
	if isSupported {
		toCheck := proposed
		// In multi-region conflict files, untouched conflict regions still contain
		// conflict markers (<<<<<<<, =======, >>>>>>>). For syntax checking of the
		// proposed change in context, temporarily collapse other conflict regions to
		// their "Ours" content so tree-sitter doesn't flag other regions' conflict markers.
		for _, other := range analysis.ConflictRegions {
			if other.Index == region.Index {
				continue
			}
			if other.StartLine >= 1 && other.EndLine <= len(origLines) {
				otherLines := origLines[other.StartLine-1 : other.EndLine]
				markerBlock := strings.Join(otherLines, "\n")
				toCheck = strings.Replace(toCheck, markerBlock, other.Ours, 1)
			}
		}
		if err := parser.ParseAndCheckSyntax(res.File, []byte(toCheck)); err != nil {
			return PreviewResult{}, &PatchError{
				Code:    resolutions.CodeInvalidPatch,
				Message: fmt.Sprintf("proposed patch introduces syntax error: %v", err),
			}
		}
	}

	// 8. Build a unified diff (read-only, informational).
	diff := buildUnifiedDiff(res.File, originalContent, proposed)

	return PreviewResult{
		ProposedContent:      proposed,
		UnifiedDiff:          diff,
		OriginalContent:      originalContent,
		RegionIndex:          region.Index,
		RequiresManualReview: !isSupported,
	}, nil
}

// findRegion returns the unique conflict region whose index matches regionID
// (formatted as fmt.Sprintf("%d", index)). Returns ErrAmbiguousRegion when
// no region matches or the regionID is malformed.
func findRegion(regions []git.ConflictRegion, regionID string) (git.ConflictRegion, error) {
	if regionID == "" {
		return git.ConflictRegion{}, ErrAmbiguousRegion
	}
	var matches []git.ConflictRegion
	for _, r := range regions {
		if fmt.Sprintf("%d", r.Index) == regionID {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return git.ConflictRegion{}, ErrAmbiguousRegion
	case 1:
		return matches[0], nil
	default:
		return git.ConflictRegion{}, &PatchError{
			Code:    resolutions.CodeInvalidPatch,
			Message: fmt.Sprintf("region ID %q is ambiguous: %d matches found", regionID, len(matches)),
		}
	}
}

// applyRegionReplacement replaces lines [region.StartLine, region.EndLine]
// (1-based, inclusive) in originalContent with replacement. All other content
// is preserved verbatim, including other conflict regions.
//
// Returns ErrInvalidPatch when the line range is out of bounds or the region
// no longer contains the expected markers.
func applyRegionReplacement(originalContent string, region git.ConflictRegion, replacement string) (string, error) {
	// Reject malformed replacements containing conflict markers.
	if strings.Contains(replacement, "<<<<<<<") || strings.Contains(replacement, "=======") || strings.Contains(replacement, ">>>>>>>") {
		return "", &PatchError{
			Code:    resolutions.CodeInvalidPatch,
			Message: "replacement contains unresolved conflict markers",
		}
	}

	lines := strings.Split(originalContent, "\n")
	// Preserve trailing newline behavior.
	trailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}

	// Reject whole-file replacements when file has content outside conflict region.
	if len(lines) > (region.EndLine - region.StartLine + 1) {
		if strings.TrimSpace(replacement) == strings.TrimSpace(originalContent) {
			return "", &PatchError{
				Code:    resolutions.CodeInvalidPatch,
				Message: "replacement is a whole-file replacement instead of targeting the conflict region",
			}
		}
	}

	// Validate bounds (1-based).
	if region.StartLine < 1 || region.EndLine < region.StartLine || region.EndLine > len(lines) {
		return "", &PatchError{
			Code: resolutions.CodeInvalidPatch,
			Message: fmt.Sprintf("region [%d,%d] is out of bounds for %d-line file",
				region.StartLine, region.EndLine, len(lines)),
		}
	}

	// Verify the first line of the region still starts with the conflict
	// opener marker so we never blindly overwrite unrelated content.
	firstLine := strings.TrimSpace(lines[region.StartLine-1])
	if !strings.HasPrefix(firstLine, "<<<<<<<") {
		return "", &PatchError{
			Code:    resolutions.CodeInvalidPatch,
			Message: fmt.Sprintf("line %d no longer contains the conflict start marker", region.StartLine),
		}
	}
	lastLine := strings.TrimSpace(lines[region.EndLine-1])
	if !strings.HasPrefix(lastLine, ">>>>>>>") {
		return "", &PatchError{
			Code:    resolutions.CodeInvalidPatch,
			Message: fmt.Sprintf("line %d no longer contains the conflict end marker", region.EndLine),
		}
	}

	// Build the result: prefix + replacement lines + suffix.
	prefix := lines[:region.StartLine-1]
	suffix := lines[region.EndLine:]

	replacementLines := strings.Split(replacement, "\n")

	out := make([]string, 0, len(prefix)+len(replacementLines)+len(suffix))
	out = append(out, prefix...)
	out = append(out, replacementLines...)
	out = append(out, suffix...)

	result := strings.Join(out, "\n")
	if trailingNewline {
		result += "\n"
	}
	return result, nil
}

// buildUnifiedDiff produces a minimal, human-readable unified diff between
// original and proposed. It uses a simple line-based comparison without
// external dependencies. The diff is informational and read-only.
func buildUnifiedDiff(filename, original, proposed string) string {
	origLines := strings.Split(original, "\n")
	propLines := strings.Split(proposed, "\n")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n", filename))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", filename))

	// Simple LCS-free hunk builder: emit context + changed lines.
	// For Phase 3 this is sufficient — a full Myers diff can replace it later.
	type hunk struct {
		origStart, origLen int
		propStart, propLen int
		lines              []string
	}

	const ctx = 3
	var hunks []hunk
	var current *hunk
	flush := func() {
		if current != nil {
			hunks = append(hunks, *current)
			current = nil
		}
	}

	i, j := 0, 0
	for i < len(origLines) || j < len(propLines) {
		ol := ""
		if i < len(origLines) {
			ol = origLines[i]
		}
		pl := ""
		if j < len(propLines) {
			pl = propLines[j]
		}
		if i < len(origLines) && j < len(propLines) && ol == pl {
			if current != nil {
				current.lines = append(current.lines, " "+ol)
				current.origLen++
				current.propLen++
				// Close hunk after ctx trailing context lines.
				if len(current.lines) > 0 {
					unchanged := 0
					for k := len(current.lines) - 1; k >= 0; k-- {
						if strings.HasPrefix(current.lines[k], " ") {
							unchanged++
						} else {
							break
						}
					}
					if unchanged > ctx {
						flush()
					}
				}
			}
			i++
			j++
			continue
		}
		// Start or continue a hunk.
		if current == nil {
			origCtxStart := i - ctx
			if origCtxStart < 0 {
				origCtxStart = 0
			}
			propCtxStart := j - ctx
			if propCtxStart < 0 {
				propCtxStart = 0
			}
			current = &hunk{
				origStart: origCtxStart + 1,
				propStart: propCtxStart + 1,
			}
			// Emit leading context.
			for k := origCtxStart; k < i; k++ {
				current.lines = append(current.lines, " "+origLines[k])
				current.origLen++
				current.propLen++
			}
		}
		if i < len(origLines) {
			current.lines = append(current.lines, "-"+origLines[i])
			current.origLen++
			i++
		}
		if j < len(propLines) {
			current.lines = append(current.lines, "+"+propLines[j])
			current.propLen++
			j++
		}
	}
	flush()

	for _, h := range hunks {
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
			h.origStart, h.origLen, h.propStart, h.propLen))
		for _, l := range h.lines {
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}
	if sb.Len() == 0 {
		return "(no changes)\n"
	}
	return sb.String()
}
