package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ConflictData struct {
	FileName     string
	BaseVersion  string
	OurVersion   string
	TheirVersion string
}

func FindGitRepositoryRoots(rootDir string) ([]string, error) {
	var repoRoots []string
	seen := make(map[string]struct{})

	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !d.IsDir() {
			return nil
		}

		if path != rootDir {
			gitDir := filepath.Join(path, ".git")
			if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
				if _, exists := seen[path]; !exists {
					seen[path] = struct{}{}
					repoRoots = append(repoRoots, path)
				}
				return filepath.SkipDir
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if info, err := os.Stat(filepath.Join(rootDir, ".git")); err == nil && info.IsDir() {
		if _, exists := seen[rootDir]; !exists {
			seen[rootDir] = struct{}{}
			repoRoots = append([]string{rootDir}, repoRoots...)
		}
	}

	if len(repoRoots) == 0 {
		curr := rootDir
		if abs, err := filepath.Abs(rootDir); err == nil {
			curr = abs
		}
		for {
			gitDir := filepath.Join(curr, ".git")
			if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
				repoRoots = append(repoRoots, curr)
				break
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
	}

	return repoRoots, nil
}

func GetConflictedFiles(repoDir string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = repoDir

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil {
		rawOutput := strings.TrimSpace(out.String())
		rawOutput = strings.ReplaceAll(rawOutput, "\r\n", "\n")
		if rawOutput != "" {
			for _, f := range strings.Split(rawOutput, "\n") {
				if f != "" {
					files = append(files, f)
					seen[f] = true
				}
			}
		}
	}

	// Scan filesystem for files containing inline conflict markers
	_ = filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "dist" || name == "build" || name == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(repoDir, path)
		if err != nil || seen[rel] {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".js", ".jsx", ".ts", ".tsx", ".go", ".py", ".java", ".c", ".cpp", ".cs", ".php", ".rb", ".json", ".md", ".txt", ".html", ".css":
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				content := string(data)
				if strings.Contains(content, "<<<<<<<") && strings.Contains(content, ">>>>>>>") {
					files = append(files, rel)
					seen[rel] = true
				}
			}
		}
		return nil
	})

	return files, nil
}

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

func ParseInlineConflict(content string) (base, ours, theirs string) {
	lines := strings.Split(content, "\n")
	var ourLines []string
	var theirLines []string
	var baseLines []string

	state := 0 // 0: Normal, 1: Ours, 2: Base (diff3), 3: Theirs

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<<<<<<<") {
			state = 1
			continue
		}
		if strings.HasPrefix(trimmed, "|||||||") {
			state = 2
			continue
		}
		if strings.HasPrefix(trimmed, "=======") && state != 0 {
			state = 3
			continue
		}
		if strings.HasPrefix(trimmed, ">>>>>>>") && state != 0 {
			state = 0
			continue
		}

		switch state {
		case 0:
			ourLines = append(ourLines, line)
			theirLines = append(theirLines, line)
			baseLines = append(baseLines, line)
		case 1:
			ourLines = append(ourLines, line)
		case 2:
			baseLines = append(baseLines, line)
		case 3:
			theirLines = append(theirLines, line)
		}
	}

	return strings.Join(baseLines, "\n"), strings.Join(ourLines, "\n"), strings.Join(theirLines, "\n")
}

func ExtractConflictVersions(repoDir, filename string) (ConflictData, error) {
	cleanFilename := strings.TrimSpace(filename)
	data := ConflictData{FileName: cleanFilename}

	getStage := func(stage int) (string, error) {
		cmd := exec.Command("git", "show", fmt.Sprintf(":%d:%s", stage, cleanFilename))
		cmd.Dir = repoDir

		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			return "", fmt.Errorf("%v (Stderr: %s)", err, stderr.String())
		}
		return out.String(), nil
	}

	var err error

	data.BaseVersion, err = getStage(1)
	if err == nil {
		data.OurVersion, _ = getStage(2)
		data.TheirVersion, _ = getStage(3)
		return data, nil
	}

	// Fallback to reading file on disk and parsing inline conflict markers
	filePath := filepath.Join(repoDir, cleanFilename)
	contentBytes, readErr := os.ReadFile(filePath)
	if readErr != nil {
		return data, fmt.Errorf("failed to get Base (Stage 1): %v and failed to read file on disk: %v", err, readErr)
	}

	content := string(contentBytes)
	if strings.Contains(content, "<<<<<<<") && strings.Contains(content, ">>>>>>>") {
		data.BaseVersion, data.OurVersion, data.TheirVersion = ParseInlineConflict(content)
		return data, nil
	}

	return data, fmt.Errorf("failed to extract conflict versions for %s: %v", cleanFilename, err)
}
