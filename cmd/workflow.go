// workflow contains the end-to-end merge pipeline used by the CLI commands.
package cmd

import (
	"fmt"
	"os"

	"commitment-issues/internal/engine"
)

// runResolutionWorkflow executes the full merge pipeline: read input files, build an AST payload,
// ask the LLM for a merged result, validate it, and write the output file.
func runResolutionWorkflow(basePath, localPath, remotePath, outputPath string) error {
	localCodeBytes, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}

	payload, err := engine.BuildASTPayload(basePath, localPath, remotePath)
	if err != nil {
		return fmt.Errorf("build AST payload: %w", err)
	}

	resolvedCode, err := engine.ResolveWithProvider(payload, string(localCodeBytes))
	if err != nil {
		return fmt.Errorf("resolve conflict: %w", err)
	}

	if err := engine.ValidateAndWrite(resolvedCode, outputPath); err != nil {
		return fmt.Errorf("validate and write: %w", err)
	}

	return nil
}
