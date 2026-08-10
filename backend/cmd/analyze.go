package cmd

import (
	"fmt"

	"CommitIssues/internal/engine"
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [path]",
	Short: "Analyze conflicts, generate AST diffs and JSON reports",
	Run: func(cmd *cobra.Command, args []string) {
		targetPath := "conflicts"
		if len(args) > 0 {
			targetPath = args[0]
		}
		scanRoot := engine.ResolveScanRoot(targetPath)
		conflictsByRepo, _, err := engine.FindConflicts(scanRoot)
		if err != nil || len(conflictsByRepo) == 0 {
			fmt.Println("No conflicted repositories found.")
			return
		}

		cfg := engine.Config{MaxConcurrency: 4, ConfidenceThreshold: 70}

		for repoRoot, files := range conflictsByRepo {
			outcomes := engine.ProcessRepository(repoRoot, files, cfg, false)
			for _, out := range outcomes {
				fmt.Print(out.Output)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
}