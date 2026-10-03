package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/api"
	"CommitIssues/internal/engine"
	"CommitIssues/internal/git"
	"CommitIssues/internal/runstate"

	"github.com/spf13/cobra"
)

var port string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the interactive AST dependency graph visualization server",
	RunE: func(cmd *cobra.Command, args []string) error {
		targetPath := "../conflicts"
		if len(args) > 0 {
			targetPath = args[0]
		}

		// serve remains AI-free: no resolver is ever invoked here. The shared
		// AI configuration is only used for metadata (provider/model shown in
		// run history) and stays env-driven, never hard-coded.
		cfg := engine.DefaultConfig()
		cfg.AI = ai.ConfigFromEnv()
		if err := cfg.Validate(); err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		scanRoot := engine.ResolveScanRoot(targetPath)
		fmt.Printf("Scanning repository for conflicts under %s...\n", scanRoot)

		conflictsByRepo, repoRoots, err := engine.FindConflicts(ctx, scanRoot)
		if err != nil {
			return fmt.Errorf("scan conflicts: %w", err)
		}

		run := runstate.NewRun()
		history := runstate.NewHistory(runstate.DefaultHistoryLimit)

		if len(repoRoots) > 0 {
			repoRoot := repoRoots[0]
			run.SetRepositoryMetadata(runstate.RepositoryMetadata{
				Name:           filepath.Base(repoRoot),
				CurrentBranch:  git.CurrentBranch(repoRoot),
				IncomingBranch: git.IncomingBranch(repoRoot),
			})
		}
		for _, repoRoot := range repoRoots {
			conflicts := conflictsByRepo[repoRoot]
			if len(conflicts) == 0 {
				continue
			}
			fmt.Printf("Loading %d conflict(s) from %s\n", len(conflicts), repoRoot)
			result, processErr := engine.ProcessRepository(ctx, run, repoRoot, conflicts, cfg, false)

			// Record the analysis run in the bounded in-memory history so the
			// History API exposes real run data.
			entry := runstate.RunHistory{
				RunID:      run.ID,
				Repository: repoRoot,
				StartedAt:  run.StartedAt,
				Provider:   cfg.AI.Provider,
				Model:      cfg.AI.Model,
			}
			if result != nil {
				entry.CompletedAt = time.Now().UTC()
				entry.FilesAnalyzed = len(result.Succeeded)
				entry.FailedFiles = len(result.Failed)
				entry.CollisionCount = collisionCountFor(run, repoRoot)
				for _, failed := range result.Failed {
					if failed.Err != nil {
						entry.ErrorSummaries = append(entry.ErrorSummaries, failed.Err.Error())
					}
				}
			}
			switch ctx.Err() {
			case context.DeadlineExceeded:
				entry.TimedOut = true
			case context.Canceled:
				entry.Cancelled = true
			}
			history.Record(entry)

			if processErr != nil {
				fmt.Printf("Warning: analysis for %s did not complete: %v\n", repoRoot, processErr)
			}
		}

		fmt.Printf("🚀 Starting Graph Server on http://localhost%s\n", port)
		api.StartGraphServer(run, history, port)
		return nil
	},
}

// collisionCountFor counts the analyzed collisions of one repository.
func collisionCountFor(run *runstate.Run, repoRoot string) int {
	total := 0
	for _, analysis := range run.AllAnalyses() {
		if analysis.Repository == repoRoot {
			total += len(analysis.SmartDiff.Collisions)
		}
	}
	return total
}

func init() {
	serveCmd.Flags().StringVarP(&port, "port", "p", ":8080", "Port to serve graph web UI")
	rootCmd.AddCommand(serveCmd)
}
