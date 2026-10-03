package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ─── Conflict region representation ──────────────────────────────────────────

// ConflictRegion is a structured, non-lossy representation of a single inline
// conflict region. Multiple regions may exist in one file, including adjacent,
// diff3 and malformed regions.
type ConflictRegion struct {
	Index       int    `json:"index"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	Ours        string `json:"ours"`
	Theirs      string `json:"theirs"`
	Base        string `json:"base,omitempty"`
	HasBase     bool   `json:"hasBase"`
	OursLabel   string `json:"oursLabel,omitempty"`
	TheirsLabel string `json:"theirsLabel,omitempty"`
	BaseLabel   string `json:"baseLabel,omitempty"`
	// Malformed is set when markers are misplaced, duplicated or nested.
	Malformed bool `json:"malformed"`
	// Incomplete is set when a region is never closed before EOF.
	Incomplete bool `json:"incomplete"`
}

// ConflictParseResult is the full, lossless result of parsing a file that may
// contain zero or more inline conflict regions.
type ConflictParseResult struct {
	Regions []ConflictRegion
	// OursText/TheirText/BaseText are the reconstructed full-file views. For
	// diff3 files BaseText holds the base section; otherwise base sections are
	// reconstructed from the surrounding common lines.
	OursText  string
	TheirText string
	BaseText  string
	// Malformed is true when any region or marker was malformed.
	Malformed bool
	// Incomplete is true when any region was not closed before EOF.
	Incomplete bool
}

// ConflictData is the payload consumed by the analysis pipeline. The legacy
// BaseVersion/OurVersion/TheirVersion fields are always derived from the
// structured Regions representation so that no conflict information is lost.
type ConflictData struct {
	FileName     string           `json:"fileName"`
	BaseVersion  string           `json:"baseVersion"`
	OurVersion   string           `json:"ourVersion"`
	TheirVersion string           `json:"theirVersion"`
	Regions      []ConflictRegion `json:"regions"`
	Malformed    bool             `json:"malformed"`
	Incomplete   bool             `json:"incomplete"`
	// Source is "staged" when the versions came from the Git index, or
	// "inline" when they were parsed from on-disk conflict markers.
	Source string `json:"source"`
	// StageValidated is true when stages 1, 2 and 3 were all present and used.
	StageValidated bool `json:"stageValidated"`
}

const (
	// SourceStaged indicates versions were extracted from the Git index.
	SourceStaged = "staged"
	// SourceInline indicates versions were parsed from inline markers on disk.
	SourceInline = "inline"
)

// ─── Typed errors ────────────────────────────────────────────────────────────

// MissingStageError is returned when a staged conflict is missing a required
// Git merge stage (1=base, 2=ours, 3=theirs).
type MissingStageError struct {
	Repository string
	File       string
	Stage      int
	Err        error
}

func (e *MissingStageError) Error() string {
	return fmt.Sprintf("missing git merge stage %d (%s) for %s in repository %s: %v",
		e.Stage, stageName(e.Stage), e.File, e.Repository, e.Err)
}

func (e *MissingStageError) Unwrap() error { return e.Err }

func stageName(stage int) string {
	switch stage {
	case 1:
		return "base"
	case 2:
		return "ours"
	case 3:
		return "theirs"
	default:
		return "unknown"
	}
}

// GitError is a structured wrapper for a failed git invocation.
type GitError struct {
	Operation string
	Dir       string
	Args      []string
	Err       error
	Stderr    string
}

func (e *GitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("git %s failed in %s: %v (stderr: %s)", e.Operation, e.Dir, e.Err, strings.TrimSpace(e.Stderr))
	}
	return fmt.Sprintf("git %s failed in %s: %v", e.Operation, e.Dir, e.Err)
}

func (e *GitError) Unwrap() error { return e.Err }

// ─── Repository discovery ────────────────────────────────────────────────────

// FindGitRepositoryRoots walks rootDir and returns every directory that looks
// like a Git repository root, deterministically sorted.
func FindGitRepositoryRoots(ctx context.Context, rootDir string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	repoRoots := make([]string, 0)
	seen := make(map[string]struct{})

	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		if path != rootDir {
			gitDir := filepath.Join(path, ".git")
			info, statErr := os.Stat(gitDir)
			if statErr == nil && info.IsDir() {
				if _, exists := seen[path]; !exists {
					seen[path] = struct{}{}
					repoRoots = append(repoRoots, path)
				}
				return filepath.SkipDir
			}
			if statErr != nil && !os.IsNotExist(statErr) {
				return fmt.Errorf("failed to stat git directory %s while searching for repository roots: %w", gitDir, statErr)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Check whether rootDir itself is a repository root. We already checked
	// the context above, so a stat permission/error here is a real discovery
	// failure and must be surfaced.
	if info, err := os.Stat(filepath.Join(rootDir, ".git")); err == nil && info.IsDir() {
		if _, exists := seen[rootDir]; !exists {
			seen[rootDir] = struct{}{}
			repoRoots = append([]string{rootDir}, repoRoots...)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to stat git directory %s while searching for repository roots: %w", filepath.Join(rootDir, ".git"), err)
	}

	if len(repoRoots) == 0 {
		curr := rootDir
		if abs, absErr := filepath.Abs(rootDir); absErr == nil {
			curr = abs
		}
		// Walk up the directory tree looking for a repository root. Permission or
		// filesystem failures here are real discovery problems.
		for {
			gitDir := filepath.Join(curr, ".git")
			if info, statErr := os.Stat(gitDir); statErr == nil && info.IsDir() {
				repoRoots = append(repoRoots, curr)
				break
			} else if statErr != nil && !os.IsNotExist(statErr) {
				return nil, fmt.Errorf("failed to stat git directory %s while searching for repository roots: %w", gitDir, statErr)
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
	}

	sort.Strings(repoRoots)
	return repoRoots, nil
}

// conflictedFileExtensions lists the extensions scanned for inline conflict
// markers when the Git index reports no unmerged entries.
var conflictedFileExtensions = map[string]struct{}{
	".js": {}, ".jsx": {}, ".ts": {}, ".tsx": {}, ".go": {}, ".py": {},
	".java": {}, ".c": {}, ".cpp": {}, ".cs": {}, ".php": {}, ".rb": {},
	".json": {}, ".md": {}, ".txt": {}, ".html": {}, ".css": {},
}

// GetConflictedFiles returns the deterministically sorted list of files that
// are conflicted in the repository, including inline-only conflicts.
//
// Every failure is propagated: a Git command failure or a filesystem walking
// failure (including permission errors) is returned as an error, never
// silently converted into an empty "no conflicts" result. Context cancellation
// is always reported as the underlying context error.
func GetConflictedFiles(ctx context.Context, repoDir string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Git discovery runs first. A failure here is a real repository failure and
	// must be surfaced rather than treated as "no conflicts".
	unmerged, err := listUnmerged(ctx, repoDir)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	files := make([]string, 0, len(unmerged))
	seen := make(map[string]bool, len(unmerged))
	for file := range unmerged {
		if !seen[file] {
			files = append(files, file)
			seen[file] = true
		}
	}

	// Scan the filesystem for explicitly non-staged fixture files that carry
	// inline conflict markers. WalkDir errors and read failures are propagated.
	walkErr := filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "build", ".next", "vendor":
				return filepath.SkipDir
			}
			return nil
		}

		rel, relErr := filepath.Rel(repoDir, path)
		if relErr != nil {
			return relErr
		}
		if seen[rel] {
			return nil
		}

		if _, ok := conflictedFileExtensions[strings.ToLower(filepath.Ext(path))]; !ok {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("failed to read %s while scanning for conflicts: %w", rel, readErr)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if result := ParseConflictRegions(string(data)); len(result.Regions) > 0 {
			files = append(files, rel)
			seen[rel] = true
		}
		return nil
	})
	if walkErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, walkErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

// listUnmerged returns the set of paths with unmerged (conflicted) index
// entries, along with the set of stages present for each path.
func listUnmerged(ctx context.Context, repoDir string) (map[string]map[int]bool, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-u", "-z")
	cmd.Dir = repoDir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &GitError{Operation: "ls-files -u", Dir: repoDir, Err: err, Stderr: stderr.String()}
	}

	result := make(map[string]map[int]bool)
	// Format per entry: "<mode> <sha> <stage>\t<path>\0"
	for _, entry := range strings.Split(out.String(), "\x00") {
		if entry == "" {
			continue
		}
		tab := strings.IndexByte(entry, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(entry[:tab])
		if len(meta) < 3 {
			continue
		}
		var stage int
		if _, scanErr := fmt.Sscanf(meta[2], "%d", &stage); scanErr != nil {
			continue
		}
		path := entry[tab+1:]
		if result[path] == nil {
			result[path] = make(map[int]bool)
		}
		result[path][stage] = true
	}
	return result, nil
}

// CurrentBranch returns the current branch name, or "unknown".
func CurrentBranch(repoDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out.String())
}

// IncomingBranch returns the branch being merged in, or "unknown".
func IncomingBranch(repoDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "MERGE_HEAD")
	cmd.Dir = repoDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out.String())
}

// ─── Inline conflict parsing ─────────────────────────────────────────────────

func markerLabel(trimmed, marker string) string {
	if len(trimmed) <= len(marker) {
		return ""
	}
	return strings.TrimSpace(trimmed[len(marker):])
}

// ParseConflictRegions parses zero or more inline conflict regions from content
// without losing any information. It supports standard conflicts, multiple and
// adjacent regions, diff3 markers, nested markers and malformed markers.
func ParseConflictRegions(content string) ConflictParseResult {
	result := ConflictParseResult{Regions: []ConflictRegion{}}

	lines := strings.Split(content, "\n")
	trailingNewline := false
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
		trailingNewline = true
	}

	var ourLines, theirLines, baseLines []string

	state := 0 // 0 normal, 1 ours, 2 base (diff3), 3 theirs
	var (
		region                               ConflictRegion
		regionOurs, regionTheirs, regionBase []string
	)

	finish := func(endLine int, incomplete bool) {
		region.Ours = strings.Join(regionOurs, "\n")
		region.Theirs = strings.Join(regionTheirs, "\n")
		region.Base = strings.Join(regionBase, "\n")
		region.EndLine = endLine
		if incomplete {
			region.Incomplete = true
			result.Incomplete = true
		}
		if region.Malformed {
			result.Malformed = true
		}
		region.Index = len(result.Regions)
		result.Regions = append(result.Regions, region)
		// The region content contributes to the reconstructed full-file views.
		ourLines = append(ourLines, regionOurs...)
		theirLines = append(theirLines, regionTheirs...)
		baseLines = append(baseLines, regionBase...)

		region = ConflictRegion{}
		regionOurs, regionTheirs, regionBase = nil, nil, nil
	}

	appendToSection := func(line string) {
		switch state {
		case 1:
			regionOurs = append(regionOurs, line)
		case 2:
			regionBase = append(regionBase, line)
		case 3:
			regionTheirs = append(regionTheirs, line)
		}
	}

	appendToAll := func(line string) {
		ourLines = append(ourLines, line)
		theirLines = append(theirLines, line)
		baseLines = append(baseLines, line)
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "<<<<<<<"):
			if state != 0 {
				// Nested start marker: preserve it and flag the region.
				region.Malformed = true
				result.Malformed = true
				appendToSection(line)
				continue
			}
			state = 1
			region = ConflictRegion{StartLine: i + 1, OursLabel: markerLabel(trimmed, "<<<<<<<")}
			regionOurs, regionTheirs, regionBase = nil, nil, nil

		case strings.HasPrefix(trimmed, "|||||||"):
			if state == 0 {
				result.Malformed = true
				appendToAll(line)
				continue
			}
			if state == 3 {
				// Diff3 base marker after the separator is malformed.
				region.Malformed = true
				result.Malformed = true
			}
			state = 2
			region.HasBase = true
			region.BaseLabel = markerLabel(trimmed, "|||||||")

		case strings.HasPrefix(trimmed, "======="):
			if state == 0 || state == 3 {
				result.Malformed = true
				appendToAll(line)
				continue
			}
			state = 3

		case strings.HasPrefix(trimmed, ">>>>>>>"):
			if state == 0 {
				result.Malformed = true
				appendToAll(line)
				continue
			}
			region.TheirsLabel = markerLabel(trimmed, ">>>>>>>")
			finish(i+1, false)
			state = 0

		default:
			if state == 0 {
				appendToAll(line)
			} else {
				appendToSection(line)
			}
		}
	}

	if state != 0 {
		// Region never closed before EOF — preserve what we captured.
		finish(len(lines), true)
	}

	render := func(lines []string) string {
		s := strings.Join(lines, "\n")
		if trailingNewline {
			s += "\n"
		}
		return s
	}
	result.OursText = render(ourLines)
	result.TheirText = render(theirLines)
	result.BaseText = render(baseLines)

	return result
}

// ParseInlineConflict parses a file's inline conflict markers into full-file
// ours/theirs/base views. It is retained for compatibility and is derived from
// the structured region representation.
func ParseInlineConflict(content string) (base, ours, theirs string) {
	result := ParseConflictRegions(content)
	return result.BaseText, result.OursText, result.TheirText
}

// ─── Conflict extraction ─────────────────────────────────────────────────────

// ExtractConflictVersions extracts the base/ours/theirs versions for a
// conflicted file. Staged conflicts require stages 1, 2 and 3 to be present;
// a missing stage yields a *MissingStageError. Files that are not staged (pure
// inline markers on disk) are exempt from stage validation.
func ExtractConflictVersions(ctx context.Context, repoDir, filename string) (ConflictData, error) {
	if err := ctx.Err(); err != nil {
		return ConflictData{}, err
	}

	cleanFilename := strings.TrimSpace(filename)

	unmerged, listErr := listUnmerged(ctx, repoDir)
	if listErr != nil {
		// Never swallow cancellation: it must surface as a context error rather
		// than being converted into an inline fixture fallback.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ConflictData{}, ctxErr
		}
	} else if stages, staged := unmerged[cleanFilename]; staged && len(stages) > 0 {
		return extractStagedVersions(ctx, repoDir, cleanFilename, stages)
	}

	// Not staged (or the Git index is unavailable, e.g. an explicitly non-staged
	// fixture directory): parse inline markers from disk.
	return extractInlineVersions(repoDir, cleanFilename)
}

func extractStagedVersions(ctx context.Context, repoDir, filename string, stages map[int]bool) (ConflictData, error) {
	data := ConflictData{FileName: filename, Source: SourceStaged, Regions: []ConflictRegion{}}

	versions := make(map[int]string, 3)
	for _, stage := range []int{1, 2, 3} {
		if !stages[stage] {
			return data, &MissingStageError{
				Repository: repoDir,
				File:       filename,
				Stage:      stage,
				Err:        fmt.Errorf("stage %d not present in index", stage),
			}
		}
		content, err := showStage(ctx, repoDir, stage, filename)
		if err != nil {
			return data, &MissingStageError{Repository: repoDir, File: filename, Stage: stage, Err: err}
		}
		versions[stage] = content
	}

	data.BaseVersion = versions[1]
	data.OurVersion = versions[2]
	data.TheirVersion = versions[3]
	data.StageValidated = true
	return data, nil
}

func extractInlineVersions(repoDir, filename string) (ConflictData, error) {
	data := ConflictData{FileName: filename, Source: SourceInline, Regions: []ConflictRegion{}}

	filePath := filepath.Join(repoDir, filename)
	contentBytes, readErr := os.ReadFile(filePath)
	if readErr != nil {
		return data, fmt.Errorf("failed to read conflicted file %s: %w", filename, readErr)
	}

	parsed := ParseConflictRegions(string(contentBytes))
	if len(parsed.Regions) == 0 {
		return data, fmt.Errorf("no conflict markers found in %s", filename)
	}

	data.Regions = parsed.Regions
	data.Malformed = parsed.Malformed
	data.Incomplete = parsed.Incomplete
	data.BaseVersion = parsed.BaseText
	data.OurVersion = parsed.OursText
	data.TheirVersion = parsed.TheirText
	return data, nil
}

func showStage(ctx context.Context, repoDir string, stage int, filename string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "show", fmt.Sprintf(":%d:%s", stage, filename))
	cmd.Dir = repoDir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &GitError{
			Operation: fmt.Sprintf("show :%d:%s", stage, filename),
			Dir:       repoDir,
			Err:       err,
			Stderr:    stderr.String(),
		}
	}
	return out.String(), nil
}
