package cmd

import (
	"fmt"

	"CommitIssues/internal/engine"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan directory for Git merge conflicts",
	Run: func(cmd *cobra.Command, args []string) {
		targetPath := ""
		if len(args) > 0 {
			targetPath = args[0]
		}
		scanRoot := engine.ResolveScanRoot(targetPath)
		fmt.Printf("Scanning for Git repositories under: %s\n\n", scanRoot)

		conflictsByRepo, repoRoots, err := engine.FindConflicts(scanRoot)
		if err != nil {
			fmt.Println("Error:", err)
			return
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
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
}