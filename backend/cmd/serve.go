package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"CommitIssues/internal/api"
	"CommitIssues/internal/engine"
	"CommitIssues/internal/git"

	"github.com/spf13/cobra"
)

var port string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the interactive AST dependency graph visualization server",
	Run: func(cmd *cobra.Command, args []string) {
		targetPath := "."
		if len(args) > 0 {
			targetPath = args[0]
		}

		scanRoot := engine.ResolveScanRoot(targetPath)
		fmt.Printf("Scanning repository for conflicts under %s...\n", scanRoot)

		conflictsByRepo, repoRoots, err := engine.FindConflicts(scanRoot)
		if err != nil {
			fmt.Printf("Error scanning for conflicts: %v\n", err)
		}

		if len(repoRoots) > 0 {
			repoRoot := repoRoots[0]
			api.SetRepositoryMetadata(api.RepositoryMetadata{
				Name:           filepath.Base(repoRoot),
				CurrentBranch:  git.CurrentBranch(repoRoot),
				IncomingBranch: git.IncomingBranch(repoRoot),
			})
		}

		if len(conflictsByRepo) > 0 {
			for _, repoRoot := range repoRoots {
				conflicts := conflictsByRepo[repoRoot]
				if len(conflicts) == 0 {
					continue
				}
				fmt.Printf("Loading %d conflict(s) from %s\n", len(conflicts), repoRoot)
				cfg := engine.Config{
					MaxConcurrency: 4,
					Provider:       "ollama",
					Model:          "qwen2:1.5b",
					BaseURL:        os.Getenv("OLLAMA_BASE_URL"),
				}
				_ = engine.ProcessRepository(repoRoot, conflicts, cfg, false)
			}
		}

		fmt.Printf("🚀 Starting Graph Server on http://localhost%s\n", port)
		api.StartGraphServer(port)
	},
}

func init() {
	serveCmd.Flags().StringVarP(&port, "port", "p", ":8080", "Port to serve graph web UI")
	rootCmd.AddCommand(serveCmd)
}
