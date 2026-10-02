package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"CommitIssues/internal/engine"
	"CommitIssues/internal/runstate"

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
	timeout     time.Duration
)

var resolveCmd = &cobra.Command{
	Use:   "resolve [path]",
	Short: "Analyze conflicts and use AI to resolve merge collisions",
	RunE: func(cmd *cobra.Command, args []string) error {
		engine.EnableDebugLogging(debug)
		start := time.Now()
		if debug {
			fmt.Printf("[resolve] starting resolve command at %s\n", start.Format(time.RFC3339))
		}

		// Fallback to environment variable if flag isn't provided
		if apiKey == "" {
			apiKey = os.Getenv("AI_API_KEY")
		}

		targetPath := "conflicts"
		if len(args) > 0 {
			targetPath = args[0]
		}

		cfg := engine.Config{
			MaxConcurrency:      concurrency,
			ConfidenceThreshold: threshold,
			Timeout:             timeout,
			APIKey:              apiKey,
			Provider:            provider,
			Model:               modelName,
			BaseURL:             baseURL,
		}
		// Configuration is validated before any scanning begins so invalid
		// settings can never start analysis or deadlock the semaphore.
		if err := cfg.Validate(); err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
		defer cancel()

		scanRoot := engine.ResolveScanRoot(targetPath)
		if debug {
			fmt.Printf("[resolve] resolved scan root: %s\n", scanRoot)
			fmt.Printf("[resolve] using provider=%s model=%s threshold=%d concurrency=%d timeout=%s\n", provider, modelName, threshold, concurrency, cfg.Timeout)
		}

		conflictsByRepo, repoRoots, err := engine.FindConflicts(ctx, scanRoot)
		if err != nil {
			return err
		}
		if len(conflictsByRepo) == 0 {
			fmt.Println("No conflicts found to resolve.")
			return nil
		}

		run := runstate.NewRun()
		failures := 0
		for _, repoRoot := range repoRoots {
			files := conflictsByRepo[repoRoot]
			if len(files) == 0 {
				continue
			}
			result, processErr := engine.ProcessRepository(ctx, run, repoRoot, files, cfg, true)
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
		if debug {
			fmt.Printf("[resolve] resolve command finished in %s\n", formatDuration(time.Since(start)))
		}
		if failures > 0 {
			return fmt.Errorf("resolve completed with %d file(s) failing; see summary above", failures)
		}
		return nil
	},
}

func init() {
	resolveCmd.Flags().StringVarP(&apiKey, "key", "k", "", "API Key for the AI provider")
	resolveCmd.Flags().StringVarP(&provider, "provider", "p", "gemini", "AI Provider (gemini, ollama, groq, openai)")
	resolveCmd.Flags().StringVarP(&modelName, "model", "m", "", "Specific model name (e.g., qwen2:1.5b, llama3, qwen2.5-coder)")
	resolveCmd.Flags().StringVar(&baseURL, "url", "", "Custom base URL (e.g., http://localhost:11434/v1)")

	resolveCmd.Flags().IntVarP(&threshold, "threshold", "t", 70, "Confidence threshold percentage")
	resolveCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Max concurrent files")
	resolveCmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Overall analysis timeout (must be > 0)")
	resolveCmd.Flags().BoolVar(&debug, "debug", false, "Enable verbose resolve pipeline logging")
	rootCmd.AddCommand(resolveCmd)
}
