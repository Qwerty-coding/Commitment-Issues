package cmd

import (
	"fmt"
	"os"
	"time"

	"CommitIssues/internal/engine"

	"github.com/spf13/cobra"
)

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

var (
	apiKey      string
	provider    string
	modelName   string
	baseURL     string
	threshold   int
	concurrency int
	debug       bool
)

var resolveCmd = &cobra.Command{
	Use:   "resolve [path]",
	Short: "Analyze conflicts and use AI to resolve merge collisions",
	Run: func(cmd *cobra.Command, args []string) {
		engine.EnableDebugLogging(debug)
		start := time.Now()
		if debug {
			fmt.Printf("[resolve] starting resolve command at %s\n", start.Format(time.RFC3339))
		}

		// Fallback to environment variable if flag isn't provided
		if apiKey == "" {
			apiKey = os.Getenv("AI_API_KEY")
		}
		if debug {
			fmt.Printf("[resolve] using provider=%s model=%s threshold=%d concurrency=%d\n", provider, modelName, threshold, concurrency)
		}

		targetPath := "conflicts"
		if len(args) > 0 {
			targetPath = args[0]
		}
		if debug {
			fmt.Printf("[resolve] scanning target: %s\n", targetPath)
		}

		scanRoot := engine.ResolveScanRoot(targetPath)
		if debug {
			fmt.Printf("[resolve] resolved scan root: %s\n", scanRoot)
		}

		conflictsByRepo, _, err := engine.FindConflicts(scanRoot)
		if err != nil || len(conflictsByRepo) == 0 {
			fmt.Println("No conflicts found to resolve.")
			return
		}

		totalFiles := 0
		for _, files := range conflictsByRepo {
			totalFiles += len(files)
		}
		if debug {
			fmt.Printf("[resolve] discovered %d repositories and %d conflicting files\n", len(conflictsByRepo), totalFiles)
		}

		cfg := engine.Config{
			MaxConcurrency:      concurrency,
			ConfidenceThreshold: threshold,
			APIKey:              apiKey,
			Provider:            provider,
			Model:               modelName,
			BaseURL:             baseURL,
		}

		for repoRoot, files := range conflictsByRepo {
			if debug {
				fmt.Printf("[resolve] processing repository %s (%d files)\n", repoRoot, len(files))
			}
			outcomes := engine.ProcessRepository(repoRoot, files, cfg, true)
			for _, out := range outcomes {
				fmt.Print(out.Output)
			}
			if debug {
				fmt.Printf("[resolve] finished repository %s in %s\n", repoRoot, formatDuration(time.Since(start)))
			}
		}
		if debug {
			fmt.Printf("[resolve] resolve command finished in %s\n", formatDuration(time.Since(start)))
		}
	},
}

func init() {
	resolveCmd.Flags().StringVarP(&apiKey, "key", "k", "", "API Key for the AI provider")
	resolveCmd.Flags().StringVarP(&provider, "provider", "p", "gemini", "AI Provider (gemini, ollama, groq, openai)")
	resolveCmd.Flags().StringVarP(&modelName, "model", "m", "", "Specific model name (e.g., qwen2:1.5b, llama3, qwen2.5-coder)")
	resolveCmd.Flags().StringVar(&baseURL, "url", "", "Custom base URL (e.g., http://localhost:11434/v1)")

	resolveCmd.Flags().IntVarP(&threshold, "threshold", "t", 70, "Confidence threshold percentage")
	resolveCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Max concurrent files")
	resolveCmd.Flags().BoolVar(&debug, "debug", false, "Enable verbose resolve pipeline logging")
	rootCmd.AddCommand(resolveCmd)
}
