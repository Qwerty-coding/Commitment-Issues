package internals

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func GetCurrBranch() (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	repoPath := strings.TrimSpace(string(output))
	return filepath.Base(repoPath), nil
}
