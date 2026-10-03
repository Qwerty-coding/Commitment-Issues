package cmd

import (
	"context"
	"fmt"
	"time"

	ai "CommitIssues/internal/ai"
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
	retries     int
	aiTimeout   time.Duration
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

		targetPath := "conflicts"
		if len(args) > 0 {
			targetPath = args[0]
		}

		cfg := engine.DefaultConfig()
		cfg.MaxConcurrency = concurrency

		// Shared AI configuration precedence: CLI flags > environment
		// variables (AI_*) > provider defaults. Nothing is hard-coded and
		// providers/models are never switched silently.
		cfg.AI = buildAIConfigFromFlags(cmd)

		// The pipeline-level confidence threshold keeps its Phase 1 meaning
		// and validation; it also drives suggestion statuses.
		cfg.ConfidenceThreshold = cfg.AI.ConfidenceThreshold
		cfg.Timeout = timeout

		// Configuration is validated before any scanning begins so invalid
		// settings can never start analysis or deadlock the semaphore.
		if err := cfg.Validate(); err != nil {
			return err
		}
		if err := cfg.AI.Validate(); err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
		defer cancel()

		scanRoot := engine.ResolveScanRoot(targetPath)
		if debug {
			fmt.Printf("[resolve] resolved scan root: %s\n", scanRoot)
			fmt.Printf("[resolve] using provider=%s model=%s threshold=%d concurrency=%d timeout=%s retries=%d aiTimeout=%s\n",
				cfg.AI.Provider, cfg.AI.Model, cfg.ConfidenceThreshold, cfg.MaxConcurrency, cfg.Timeout, cfg.AI.RetryCount, cfg.AI.RequestTimeout)
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

// buildAIConfigFromFlags assembles the shared AI configuration with the
// documented precedence: explicitly-set CLI flags override environment
// variables, which override provider defaults. Unset flags never mask env
// configuration and no provider/model is ever switched silently.
func buildAIConfigFromFlags(cmd *cobra.Command) ai.Config {
	cfg := ai.ConfigFromEnv()
	flags := cmd.Flags()
	if flags.Changed("provider") {
		cfg.Provider = provider
	}
	if flags.Changed("model") {
		cfg.Model = modelName
	}
	if flags.Changed("url") {
		cfg.BaseURL = baseURL
	}
	if flags.Changed("key") {
		cfg.APIKey = apiKey
	}
	if flags.Changed("threshold") {
		cfg.ConfidenceThreshold = threshold
	}
	if flags.Changed("retries") {
		cfg.RetryCount = retries
	}
	if flags.Changed("ai-timeout") {
		cfg.RequestTimeout = aiTimeout
	}
	return cfg
}

func init() {
	resolveCmd.Flags().StringVarP(&apiKey, "key", "k", "", "API Key for the AI provider (env: AI_API_KEY)")
	resolveCmd.Flags().StringVarP(&provider, "provider", "p", "", "AI Provider (ollama, gemini, groq; env: AI_PROVIDER; default: ollama)")
	resolveCmd.Flags().StringVarP(&modelName, "model", "m", "", "Specific model name (env: AI_MODEL; default: qwen2:1.5b for ollama)")
	resolveCmd.Flags().StringVar(&baseURL, "url", "", "Custom base URL (env: AI_BASE_URL)")
	resolveCmd.Flags().IntVarP(&threshold, "threshold", "t", ai.DefaultConfidenceThreshold, "Confidence threshold percentage (env: AI_CONFIDENCE_THRESHOLD)")
	resolveCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Max concurrent files")
	resolveCmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Overall analysis timeout (must be > 0)")
	resolveCmd.Flags().IntVar(&retries, "retries", ai.DefaultRetryCount, "Retries per AI request for transient failures (env: AI_RETRIES)")
	resolveCmd.Flags().DurationVar(&aiTimeout, "ai-timeout", ai.DefaultRequestTimeout, "Per-request AI timeout (env: AI_TIMEOUT)")
	resolveCmd.Flags().BoolVar(&debug, "debug", false, "Enable verbose resolve pipeline logging")
	rootCmd.AddCommand(resolveCmd)
}
