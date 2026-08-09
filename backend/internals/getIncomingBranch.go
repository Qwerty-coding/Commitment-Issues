package internals

import (
	"os/exec"
	"strings"
)

func GetIncomingBranch() (string, error) {

	cmd := exec.Command(
		"git",
		"rev-parse",
		"--abbrev-ref",
		"MERGE_HEAD",
	)

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	incomingPath := strings.TrimSpace(string(output))
	return incomingPath, nil
}