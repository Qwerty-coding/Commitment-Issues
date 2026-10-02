package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

		cfg := engine.DefaultConfig()
		cfg.Provider = "ollama"
		cfg.Model = "qwen2:1.5b"
		cfg.BaseURL = os.Getenv("OLLAMA_BASE_URL")
		if err := cfg.Validate(); err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		scanRoot := engine.ResolveScanRoot(targetPath)
		fmt.Printf("Scanning repository for conflicts under %s...\n", scanRoot)

		conflictsByRepo, repoRoots, err := engine.FindConflicts(ctx, scanRoot)
		if err != nil {
			fmt.Printf("Error scanning for conflicts: %v\n", err)
		}

		run := runstate.NewRun()

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
			if _, processErr := engine.ProcessRepository(ctx, run, repoRoot, conflicts, cfg, false); processErr != nil {
				fmt.Printf("Warning: analysis for %s did not complete: %v\n", repoRoot, processErr)
			}
		}

		fmt.Printf("🚀 Starting Graph Server on http://localhost%s\n", port)
		api.StartGraphServer(run, port)
		return nil
	},
}

func init() {
	serveCmd.Flags().StringVarP(&port, "port", "p", ":8080", "Port to serve graph web UI")
	rootCmd.AddCommand(serveCmd)
}
