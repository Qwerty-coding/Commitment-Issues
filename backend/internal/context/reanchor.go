package promptcontext

import (
	"fmt"
	"strings"

	"CommitIssues/internal/fileutil"
	git "CommitIssues/internal/git"
)

// ReanchorResult describes the outcome of re-anchoring a stored analysis after
// one conflict region was resolved and written to disk.
type ReanchorResult struct {
	// Analysis is the updated snapshot (new ContentHash, updated region line
	// numbers and RegionContextHashes).
	Analysis FileAnalysis
	// MappedRegionIDs maps the OLD region index (as string) to the NEW region
	// index (as string) for every region that could be matched unambiguously.
	// The applied region is simply absent from this map.
	MappedRegionIDs map[string]string
	// OK reports whether re-anchoring produced a consistent mapping. When
	// false, callers MUST fall back to invalidating (re-analysis) instead of
	// trusting the updated snapshot.
	OK bool
}

// regionSignature is the identity used to match a pre-apply region to its
// post-apply counterpart. It uses the full marker block (ours+base+theirs)
// which is byte-identical for every region that was NOT the one applied.
func regionSignature(r git.ConflictRegion) string {
	var sb strings.Builder
	sb.WriteString(r.Ours)
	sb.WriteString("\x00")
	sb.WriteString(r.Base)
	sb.WriteString("\x00")
	sb.WriteString(r.Theirs)
	return sb.String()
}

// ReanchorAnalysis re-parses newContent (the file AFTER one region was applied)
// and produces an updated FileAnalysis that keeps the still-present conflict
// regions valid, so their already-generated AI suggestions do NOT need to be
// regenerated.
//
// Every old region must match exactly one new region by its full marker-block
// signature; the applied region is simply absent from the new file and matches
// nothing. The mapping is accepted only when exactly one old region is missing,
// every new region is accounted for, and no duplicate signature makes the
// survivor ambiguous. Otherwise OK is false and the caller must invalidate.
func ReanchorAnalysis(prev FileAnalysis, newContent []byte) ReanchorResult {
	res := ReanchorResult{Analysis: prev, MappedRegionIDs: map[string]string{}, OK: false}

	newParsed := git.ParseConflictRegions(string(newContent))
	newRegions := newParsed.Regions

	// Index new regions by signature. A signature shared by more than one new
	// region is ambiguous and cannot be matched safely.
	newBySig := make(map[string][]int, len(newRegions))
	for i, r := range newRegions {
		sig := regionSignature(r)
		newBySig[sig] = append(newBySig[sig], i)
	}

	mapped := make(map[string]string, len(prev.ConflictRegions))
	usedNew := make(map[int]struct{}, len(newRegions))
	unmatched := 0

	// Walk old regions in order; each must match exactly one unused new region.
	for _, old := range prev.ConflictRegions {
		candidates := newBySig[regionSignature(old)]
		// Filter to unused candidates.
		var available []int
		for _, idx := range candidates {
			if _, taken := usedNew[idx]; !taken {
				available = append(available, idx)
			}
		}
		if len(available) != 1 {
			unmatched++
			continue
		}
		newIdx := available[0]
		usedNew[newIdx] = struct{}{}
		mapped[fmt.Sprintf("%d", old.Index)] = fmt.Sprintf("%d", newRegions[newIdx].Index)
	}

	// Exactly one old region (the applied one) may be absent from the new file.
	// Two or more unmatched old regions means the mapping is not trustworthy
	// (e.g. two regions became byte-identical), so the caller must invalidate.
	if unmatched > 1 {
		return res
	}

	// Every new region must be accounted for: a leftover unmatched new region
	// means a region appeared that we cannot explain (e.g. the applied
	// replacement itself introduced conflict markers).
	if len(usedNew) != len(newRegions) {
		return res
	}

	// Signature multiset guard: when duplicate region signatures exist, the
	// greedy first-match above can silently associate an old region with the
	// wrong new twin. Verify that the multiset of MATCHED old signatures equals
	// the multiset of new signatures; any discrepancy means the surviving region
	// is ambiguous and the snapshot must not be trusted.
	if !signatureMultisetsAgree(prev.ConflictRegions, mapped, newRegions) {
		return res
	}

	// Rebuild the analysis with the new regions and hashes.
	updated := prev
	updated.ConflictRegions = newRegions
	updated.ContentHash = fileutil.HashContent(newContent)
	updated.RegionContextHashes = regionHashesFor(newContent, newRegions)

	res.Analysis = updated
	res.MappedRegionIDs = mapped
	res.OK = true
	return res
}

// signatureMultisetsAgree reports whether the mapping is unambiguous when
// duplicate region signatures exist. If a signature appears more than once
// among the old regions AND is still present in the new file, we cannot know
// which old twin survived (and therefore cannot safely remap its RegionID), so
// the mapping is rejected.
func signatureMultisetsAgree(old []git.ConflictRegion, mapped map[string]string, newRegions []git.ConflictRegion) bool {
	oldCount := make(map[string]int)
	for _, o := range old {
		oldCount[regionSignature(o)]++
	}
	for _, n := range newRegions {
		if oldCount[regionSignature(n)] > 1 {
			return false
		}
	}

	// The multiset of matched old signatures must equal the new signatures.
	matched := make(map[string]int)
	for _, o := range old {
		if _, ok := mapped[fmt.Sprintf("%d", o.Index)]; !ok {
			continue // the applied (absent) region
		}
		matched[regionSignature(o)]++
	}
	for _, n := range newRegions {
		sig := regionSignature(n)
		if matched[sig] == 0 {
			return false
		}
		matched[sig]--
	}
	for _, count := range matched {
		if count != 0 {
			return false
		}
	}
	return true
}

// regionHashesFor computes per-region context hashes for the given regions
// against the file content, keyed by fmt.Sprintf("%d", region.Index).
func regionHashesFor(content []byte, regions []git.ConflictRegion) map[string]string {
	out := make(map[string]string, len(regions))
	if len(regions) == 0 {
		return out
	}
	allLines := strings.Split(string(content), "\n")
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	for _, r := range regions {
		out[fmt.Sprintf("%d", r.Index)] = fileutil.RegionContextHash(allLines, r)
	}
	return out
}
