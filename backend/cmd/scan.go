package cmd

import (
	"fmt"

	"CommitIssues/internal/engine"

	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan directory for Git merge conflicts",
	RunE: func(cmd *cobra.Command, args []string) error {
		targetPath := "conflicts"
		if len(args) > 0 {
			targetPath = args[0]
		}
		scanRoot := engine.ResolveScanRoot(targetPath)
		fmt.Printf("Scanning for Git repositories under: %s\n\n", scanRoot)

		conflictsByRepo, repoRoots, err := engine.FindConflicts(cmd.Context(), scanRoot)
		if err != nil {
			return err
		}

		for _, repoRoot := range repoRoots {
			conflicts := conflictsByRepo[repoRoot]
			if len(conflicts) == 0 {
				fmt.Printf("%s: No conflicts\n", repoRoot)
				continue
			}
			fmt.Printf("%s: %d conflicted file(s)\n", repoRoot, len(conflicts))
			for _, file := range conflicts {
				fmt.Printf("  - %s\n", file)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
}
