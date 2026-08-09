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

	return repoRoots, nil
}

func GetConflictedFiles(repoDir string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = repoDir

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %s", err, stderr.String())
	}

	rawOutput := strings.TrimSpace(out.String())
	rawOutput = strings.ReplaceAll(rawOutput, "\r\n", "\n")

	files := strings.Split(rawOutput, "\n")

	if len(files) == 1 && files[0] == "" {
		return []string{}, nil
	}

	return files, nil
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
	if err != nil {
		return data, fmt.Errorf("failed to get Base (Stage 1): %v", err)
	}

	data.OurVersion, err = getStage(2)
	if err != nil {
		return data, fmt.Errorf("failed to get Ours (Stage 2): %v", err)
	}

	data.TheirVersion, err = getStage(3)
	if err != nil {
		return data, fmt.Errorf("failed to get Theirs (Stage 3): %v", err)
	}

	return data, nil
}
