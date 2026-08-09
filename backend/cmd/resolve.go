package cmd

import (
	"fmt"
	"os"

	"CommitIssues/internal/engine"
	"github.com/spf13/cobra"
)

var (
	apiKey      string
	provider    string
	modelName   string
	baseURL     string
	threshold   int
	concurrency int
)

var resolveCmd = &cobra.Command{
	Use:   "resolve [path]",
	Short: "Analyze conflicts and use AI to resolve merge collisions",
	Run: func(cmd *cobra.Command, args []string) {
		// Fallback to environment variable if flag isn't provided
		if apiKey == "" {
			apiKey = os.Getenv("AI_API_KEY")
		}

		targetPath := ""
		if len(args) > 0 {
			targetPath = args[0]
		}
		scanRoot := engine.ResolveScanRoot(targetPath)
		conflictsByRepo, _, err := engine.FindConflicts(scanRoot)
		if err != nil || len(conflictsByRepo) == 0 {
			fmt.Println("No conflicts found to resolve.")
			return
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
			outcomes := engine.ProcessRepository(repoRoot, files, cfg, true)
			for _, out := range outcomes {
				fmt.Print(out.Output)
			}
		}
	},
}

func init() {
	resolveCmd.Flags().StringVarP(&apiKey, "key", "k", "", "API Key for the AI provider")
	resolveCmd.Flags().StringVarP(&provider, "provider", "p", "gemini", "AI Provider (gemini, ollama, groq, openai)")
	resolveCmd.Flags().StringVarP(&modelName, "model", "m", "", "Specific model name (e.g., llama3, qwen2.5-coder)")
	resolveCmd.Flags().StringVar(&baseURL, "url", "", "Custom base URL (e.g., http://localhost:11434/v1)")
	
	resolveCmd.Flags().IntVarP(&threshold, "threshold", "t", 70, "Confidence threshold percentage")
	resolveCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Max concurrent files")
	rootCmd.AddCommand(resolveCmd)
}