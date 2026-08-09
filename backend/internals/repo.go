package internals

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func GetRepoName() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	repoPath := strings.TrimSpace(string(output))
	return filepath.Base(repoPath), nil

}
