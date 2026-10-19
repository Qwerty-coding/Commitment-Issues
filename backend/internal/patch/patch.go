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
				markerBlockCRLF := strings.ReplaceAll(markerBlock, "\n", "\r\n")
				toCheck = strings.Replace(toCheck, markerBlockCRLF, other.Ours, 1)
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

// BuildProposedContent splices replacement into originalContent at the given
// conflict region and returns the full resulting file content. It is the
// exported entry point for callers that need the complete proposed file (e.g.
// deterministic verification's syntax check) without building a diff.
//
// Returns ErrInvalidPatch when the region cannot be located or the replacement
// cannot be safely mapped.
func BuildProposedContent(originalContent string, region git.ConflictRegion, replacement string) (string, error) {
	return applyRegionReplacement(originalContent, region, replacement)
}

// applyRegionReplacement replaces lines [region.StartLine, region.EndLine]
// (1-based, inclusive) in originalContent with replacement. All other content
// is preserved verbatim, including other conflict regions.
//
// When the model returns the WHOLE enclosing block (e.g. the entire function)
// while the conflict spans only its inner lines, the replacement is first
// normalized back to the region-sized slice (see normalizeToConflictRegion)
// so the splice can never duplicate the surrounding code.
//
// Returns ErrInvalidPatch when the line range is out of bounds or the region
// no longer contains the expected markers.
func applyRegionReplacement(originalContent string, region git.ConflictRegion, replacement string) (string, error) {
	// Strip accidental markdown fences (e.g. ```go\n...```) emitted by LLMs.
	replacement = cleanMarkdownFences(replacement)

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

	// Filter out conflict markers + duplicated enclosing code from the
	// replacement before splicing (see above).
	replacement = normalizeToConflictRegion(replacement, region, lines)

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

// normalizeToConflictRegion guards against the AI returning the WHOLE
// enclosing block (e.g. the entire function) while the conflict region spans
// only the inner lines between the markers. Splicing such a replacement into
// the region would duplicate the surrounding code and corrupt the file.
//
// Detection: the replacement is considered an enclosing-block replacement
// when the original file content BEFORE the region contains a line that is
// also the first non-empty line of the replacement (e.g. the function's
// original signature), with that context line located strictly before the
// region. In that case the replacement's slice starting at the first context
// match and ending at the matching context line AFTER the region (or EOF) is
// cut back to what lies strictly inside the enclosing block's braces around
// the region — returned to the caller as just the region replacement.
//
// When no such duplication is detectable, replacement is returned unchanged.
func normalizeToConflictRegion(replacement string, region git.ConflictRegion, allLines []string) string {
	replacementLines := strings.Split(replacement, "\n")
	firstRep := firstNonEmptyLine(replacementLines)
	if firstRep < 0 {
		return replacement
	}
	if region.StartLine <= 1 {
		return replacement
	}

	// Simplest robust guard: if the replacement's first/last non-empty lines
	// both also appear as context lines around the region in the ORIGINAL
	// file, and the replacement contains the region's own content, then the
	// model reproduced the enclosing function. Slice it back to the section
	// between the enclosing block's first and last context lines, exclusive.
	// The inner body is derived from the replacement (not from the old file)
	// while the enclosing context is already present in the file, so splicing
	// the enclosing block would duplicate it.
	lastRep := lastNonEmptyLine(replacementLines)
	if lastRep < firstRep || lastRep == firstRep {
		return replacement
	}
	firstRepLine := strings.TrimSpace(replacementLines[firstRep])
	lastRepLine := strings.TrimSpace(replacementLines[lastRep])
	if firstRepLine == "" || lastRepLine == "" {
		return replacement
	}
	beforeIdx := findLineBefore(allLines, firstRepLine, region.StartLine-1)
	afterIdx := findLineAfter(allLines, lastRepLine, region.EndLine, len(allLines))
	if beforeIdx < 0 || afterIdx < 0 {
		// The first/last replacement lines are not context duplicates around
		// the region in the original file — this is a genuine region-sized
		// replacement; splice it as-is.
		return replacement
	}

	// Find the enclosing block boundaries: the replacement (whole block)
	// spans [beforeIdx+1 .. afterIdx+1] in 1-based terms — i.e. it duplicates
	// the file content between the context lines. The region replacement is
	// everything strictly inside the block: remove the first context line, the
	// last context line, and any duplicate of the original region content
	// already inside the AI's block so the merged result keeps exactly one
	// copy of the resolved body.
	inner := replacementLines[firstRep+1 : lastRep]
	inner = trimTopLevelBraces(inner)

	// Drop a verbatim copy of the old region content inside the replacement:
	// an enclosing replacement often reproduces ours/theirs verbatim.
	inner = dropEmbeddedRegion(inner, region)

	if len(inner) == 0 {
		return replacement // nothing left: bail out unchanged (invalid content)
	}
	return strings.Join(inner, "\n")
}

// dropEmbeddedRegion removes a verbatim copy of the old ours-side region
// content (a full duplicate of lines the file already contains around the
// region) from the replacement inner slice, so an enclosing replacement
// doesn't duplicate the unresolved ours-side lines.
func dropEmbeddedRegion(inner []string, region git.ConflictRegion) []string {
	oursLines := strings.Split(region.Ours, "\n")
	if len(oursLines) < 3 || len(inner) < len(oursLines) {
		return inner
	}
	for start := 0; start+len(oursLines) <= len(inner); start++ {
		match := true
		for i, ol := range oursLines {
			if strings.TrimSpace(inner[start+i]) != strings.TrimSpace(ol) {
				match = false
				break
			}
		}
		if match {
			out := make([]string, 0, len(inner)-len(oursLines))
			out = append(out, inner[:start]...)
			out = append(out, inner[start+len(oursLines):]...)
			return out
		}
	}
	return inner
}

// trimTopLevelBraces removes a dangling "{" before and "}" after the inner
// body of an enclosing block replacement.
func trimTopLevelBraces(inner []string) []string {
	start, end := 0, len(inner)-1
	for start <= end && strings.TrimSpace(inner[start]) == "" {
		start++
	}
	for end >= start && strings.TrimSpace(inner[end]) == "" {
		end--
	}
	if start > end {
		return inner
	}
	if strings.TrimSpace(inner[start]) == "{" {
		start++
	}
	for end >= start {
		t := strings.TrimSpace(inner[end])
		if t == "" {
			end--
			continue
		}
		if t == "}" {
			end--
			break
		}
		break
	}
	if start > end+1 {
		return inner
	}
	return inner[start : end+1]
}

// findLineBefore returns the 0-based index of the last line before maxIdx
// (exclusive) whose trimmed content equals target, or -1.
func findLineBefore(lines []string, target string, maxIdx int) int {
	for i := maxIdx - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == target {
			return i
		}
	}
	return -1
}

// findLineAfter returns the 0-based index of the first line at or after
// minIdx whose trimmed content equals target, or -1.
func findLineAfter(lines []string, target string, minIdx, maxIdx int) int {
	for i := minIdx; i < maxIdx && i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == target {
			return i
		}
	}
	return -1
}

// firstNonEmptyLine returns the index of the first non-blank line, or -1.
func firstNonEmptyLine(lines []string) int {
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			return i
		}
	}
	return -1
}

// lastNonEmptyLine returns the index of the last non-blank line, or -1.
func lastNonEmptyLine(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

// indexOfLine returns the 0-based index of the first line equal to target
// within the first maxIdx lines, or -1.
func indexOfLine(lines []string, target string, maxIdx int) int {
	for i := 0; i < maxIdx && i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == target {
			return i
		}
	}
	return -1
}

// cleanMarkdownFences strips accidental markdown code block fences (e.g. ```go\n...```)
// that LLMs often emit.
func cleanMarkdownFences(s string) string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "```") {
		if newlineIdx := strings.Index(trimmed, "\n"); newlineIdx != -1 {
			trimmed = trimmed[newlineIdx+1:]
		} else {
			trimmed = strings.TrimPrefix(trimmed, "```")
		}
		if idx := strings.LastIndex(trimmed, "```"); idx != -1 {
			trimmed = trimmed[:idx]
		}
		return strings.TrimRight(trimmed, " \t\r\n")
	}
	return s
}
