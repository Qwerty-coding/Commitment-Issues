package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	ai "CommitIssues/internal/ai"
	"CommitIssues/internal/api"
	"CommitIssues/internal/engine"
	"CommitIssues/internal/git"
	"CommitIssues/internal/runstate"
	"CommitIssues/internal/validation"

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
		cfg.Validation = validation.ConfigFromEnv()
		if err := cfg.Validate(); err != nil {
			return err
		}

		// Cancellable server context: SIGINT/SIGTERM (or cmd.Context()
		// cancellation) triggers a graceful HTTP shutdown with in-flight
		// request draining inside api.StartGraphServer.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		scanRoot := engine.ResolveScanRoot(targetPath)
		fmt.Printf("Scanning repository for conflicts under %s...\n", scanRoot)

		conflictsByRepo, repoRoots, err := engine.FindConflicts(ctx, scanRoot)
		if err != nil {
			return fmt.Errorf("scan conflicts: %w", err)
		}

		run := runstate.NewRun()
		if len(cfg.Validation.AllowList) > 0 {
			run.SetValidationConfig(cfg.Validation)
		}
		// Durable history is opt-in via HISTORY_PERSIST_PATH; otherwise the store
	// stays bounded and in-memory as before.
	history := runstate.NewHistoryFromEnv(runstate.DefaultHistoryLimit)

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

		// Capture AST cache statistics after processing so the API can surface
		// them via /api/repository.
		if cfg.ASTCache != nil {
			run.SetCacheStats(cfg.ASTCache.Stats())
		}

		fmt.Printf("🚀 Starting Graph Server on http://localhost%s\n", port)
		api.StartGraphServer(ctx, run, history, port)
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
