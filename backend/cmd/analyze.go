package cmd

import (
	"context"
	"fmt"

	"CommitIssues/internal/engine"
	"CommitIssues/internal/runstate"
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [path]",
	Short: "Analyze conflicts, generate AST diffs and JSON reports",
	RunE: func(cmd *cobra.Command, args []string) error {
		targetPath := "conflicts"
		if len(args) > 0 {
			targetPath = args[0]
		}

		cfg := engine.DefaultConfig()
		if err := cfg.Validate(); err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
		defer cancel()

		scanRoot := engine.ResolveScanRoot(targetPath)
		conflictsByRepo, repoRoots, err := engine.FindConflicts(ctx, scanRoot)
		if err != nil {
			return err
		}
		if len(conflictsByRepo) == 0 {
			fmt.Println("No conflicted repositories found.")
			return nil
		}

		run := runstate.NewRun()
		failures := 0
		for _, repoRoot := range repoRoots {
			files := conflictsByRepo[repoRoot]
			if len(files) == 0 {
				continue
			}
			result, processErr := engine.ProcessRepository(ctx, run, repoRoot, files, cfg, false)
			if result != nil {
				for _, out := range result.Outcomes {
					fmt.Print(out.Output)
				}
				fmt.Print(result.Summary())
				failures += len(result.Failed)
			}
			if processErr != nil {
				return processErr
			}
		}

		if failures > 0 {
			return fmt.Errorf("analysis completed with %d file(s) failing; see summary above", failures)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
}
