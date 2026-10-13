// Package verification computes an objective, deterministic Local
// Verification Score (0-100) for a proposed conflict resolution. It measures
// observable properties of the proposed replacement — never the model's
// self-reported confidence, which per the recorded design decision
// (docs/design_decisions.md, 2026-10-05) is post-hoc rationalization and must
// not gate suggestion status.
//
// Weights:
//
//	Marker cleanliness ....... 15  (all conflict markers absent)
//	Syntax validity .......... 35  (proposed content parses / symbols absent)
//	Content sanity ........... 20  (bounded size vs ours/theirs; untruncated)
//	Content preservation ..... 30  (retains non-colliding context lines)
//
// A score below the configured threshold flags the suggestion for manual
// review instead of trusting the model's self-assessment.
package verification

import (
	"fmt"
	"strings"

	git "CommitIssues/internal/git"
	parser "CommitIssues/internal/parser"
)

// Status values mirror the suggestion statuses consumed by runstate.
const (
	StatusVerified      = "verified"
	StatusNeedsReview   = "needs_review"
	StatusUnconfigurable = "unconfigurable"
)

// Result is the deterministic verification verdict for one replacement.
type Result struct {
	// Score is the local verification score in the 0-100 range.
	Score int `json:"score"`
	// Status is one of the Status constants above.
	Status string `json:"status"`
	// Findings lists each failed check with its weight loss, deterministically
	// ordered by check name.
	Findings []Finding `json:"findings,omitempty"`
}

// Finding is one failed local verification check.
type Finding struct {
	Check   string `json:"check"`
	Weight  int    `json:"weight"`
	Details string `json:"details,omitempty"`
}

// Evaluate computes the Local Verification Score for proposed replacement of
// a single conflict region.
//
// Parameters:
//   - file: the conflicted file (language derived from the extension).
//   - fullFileAfter: the complete proposed file content after normalizing the
//     region to the replacement (may be empty when the caller cannot build it;
//     the region-level checks below still run).
//   - region: the conflict region being replaced.
func Evaluate(file, fullFileAfter string, region git.ConflictRegion) Result {
	findings := make([]Finding, 0, 4)
	score := 100

	// 1. Marker cleanliness (15): no conflict markers may survive.
	if hasConflictMarkers(strings.TrimSpace(fullFileAfter)) {
		score -= 15
		findings = append(findings, Finding{Check: "markers", Weight: 15, Details: "proposed content still contains conflict markers"})
	}

	// 2. Syntax validity (35): only when the file language is supported.
	if parser.IsSupportedLanguage(file) {
		if err := parser.ParseAndCheckSyntax(file, []byte(fullFileAfter)); err != nil {
			score -= 35
			findings = append(findings, Finding{Check: "syntax", Weight: 35, Details: err.Error()})
		}
	}

	// 3. Content sanity (20): replacement bounded relative to ours/theirs.
	if w, details := sanityExcess(region, fullFileAfter); w > 0 {
		score -= w
		findings = append(findings, Finding{Check: "sanity", Weight: w, Details: details})
	}

	// 4. Content preservation (30): non-conflicted lines known to both sides
	//    should survive — measured by diff-sized replacement vs region.
	if w, details := preservationLoss(region, fullFileAfter); w > 0 {
		score -= w
		findings = append(findings, Finding{Check: "preservation", Weight: w, Details: details})
	}

	if score < 0 {
		score = 0
	}
	status := StatusVerified
	if score < 70 {
		status = StatusNeedsReview
	}
	return Result{Score: score, Status: status, Findings: findings}
}

func hasConflictMarkers(content string) bool {
	return strings.Contains(content, "<<<<<<<") ||
		strings.Contains(content, "=======") ||
		strings.Contains(content, ">>>>>>>")
}

// sanityExcess penalizes replacements that look truncated (0-size) or
// runaway (larger than both sides by a wide margin). Weight 20.
func sanityExcess(region git.ConflictRegion, fullFileAfter string) (int, string) {
	ours, theirs := len(strings.TrimSpace(region.Ours)), len(strings.TrimSpace(region.Theirs))
	proposed := len(strings.TrimSpace(fullFileAfter))
	replacementIsRegion := strings.TrimSpace(region.Ours) != "" || strings.TrimSpace(region.Theirs) != ""
	if !replacementIsRegion {
		return 0, ""
	}
	if proposed == 0 {
		return 20, "replacement is empty"
	}
	// Truncation guard: far smaller than either side.
	smallestSide := ours
	if theirs < smallestSide {
		smallestSide = theirs
	}
	if smallestSide >= 60 && proposed < smallestSide/3 { // >= 33% of the smaller side
		return 10, fmt.Sprintf("replacement is %d bytes, far below the conflicted content sizes (%d/%d)", proposed, ours, theirs)
	}
	// Runaway guard: far larger than both sides.
	largest := ours
	if theirs > largest {
		largest = theirs
	}
	if largest > 0 && proposed > largest*10 {
		return 10, fmt.Sprintf("replacement is %d bytes, far larger than the conflicted content sizes (%d/%d)", proposed, ours, theirs)
	}
	return 0, ""
}

// preservationLoss penalizes replacements that discard substantially more
// content than the conflict itself accounts for. Weight 30.
func preservationLoss(region git.ConflictRegion, fullFileAfter string) (int, string) {
	ours := countNonBlankLines(region.Ours)
	theirs := countNonBlankLines(region.Theirs)
	proposed := countNonBlankLines(fullFileAfter)
	expectedMin := ours
	if theirs < expectedMin {
		expectedMin = theirs
	}
	if expectedMin > 2 && proposed*3 < expectedMin { // keeps < ~1/3 of the content lines
		return 15, fmt.Sprintf("replacement keeps %d of ~%d content lines — likely truncated or discarded", proposed, expectedMin)
	}
	return 0, ""
}

func countNonBlankLines(text string) int {
	n := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}
