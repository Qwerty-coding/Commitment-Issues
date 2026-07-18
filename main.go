package main

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// Config holds the runtime configuration passed down to your phases
type Config struct {
	BaseFilePath   string
	LocalFilePath  string
	RemoteFilePath string
	OutputFilePath string
	GeminiAPIKey   string
}

// Global config instance populated by Cobra flags
var cfg Config

func main() {
	// Attempt to load .env variables quietly to preserve your previous setup
	_ = godotenv.Load()

	// 1. Define the Root Command
	var rootCmd = &cobra.Command{
		Use:   "merge-resolver",
		Short: "A semantic AST-based merge conflict resolver using Gemini LLM",
		Long:  `Merge Resolver uses Tree-Sitter and LLMs to semantically resolve complex Git merge conflicts safely.`,
	}

	// 2. Define the 'resolve' Subcommand
	var resolveCmd = &cobra.Command{
		Use:   "resolve",
		Short: "Resolve a 3-way merge conflict",
		Run: func(cmd *cobra.Command, args []string) {
			// Fallback: If API key wasn't passed as a flag, check the environment variables
			if cfg.GeminiAPIKey == "" {
				cfg.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")
			}
			if cfg.GeminiAPIKey == "" {
				log.Fatal("FATAL: GEMINI_API_KEY is missing. Pass via --api-key flag or set in .env")
			}

			ctx := context.Background()
			log.Println("Starting Merge Conflict Resolver CLI...")

			// PHASE 1: Deterministic Line-Based Filter
			log.Println("Phase 1: Attempting deterministic merge...")
			if attemptDeterministicMerge(cfg) {
				log.Println("Phase 1 Succeeded: No structural overlaps. Exiting 0.")
				os.Exit(0)
			}

			// PHASE 2 & 3: AST Analysis & Conflict Isolation
			log.Println("Phase 2 & 3: Parsing ASTs and Isolating Conflicts...")
			conflictNode, err := IsolateConflicts(ctx, cfg)
			if err != nil {
				log.Fatalf("Conflict isolation failed: %v", err)
			}

			// PHASE 4: Surgical LLM Prompting via Gemini
			log.Println("Phase 4: Sending isolated conflict to Gemini API...")
			resolvedCode, err := ResolveWithGemini(ctx, conflictNode, cfg.GeminiAPIKey)
			if err != nil {
				log.Fatalf("LLM Resolution failed: %v", err)
			}

			// PHASE 5: Tree-Sitter Guard & Write
			log.Println("Phase 5: Validating syntax and writing file...")
			err = ValidateAndWrite([]byte(resolvedCode), cfg.OutputFilePath)
			if err != nil {
				log.Fatalf("Phase 5 Guard Failed: %v", err)
			}

			log.Println("Merge completed successfully.")
			os.Exit(0)
		},
	}

	// 3. Bind CLI Flags to the Config struct
	resolveCmd.Flags().StringVarP(&cfg.BaseFilePath, "base", "b", "base.rs", "Path to base file")
	resolveCmd.Flags().StringVarP(&cfg.LocalFilePath, "local", "l", "local.rs", "Path to local file")
	resolveCmd.Flags().StringVarP(&cfg.RemoteFilePath, "remote", "r", "remote.rs", "Path to remote file")
	resolveCmd.Flags().StringVarP(&cfg.OutputFilePath, "output", "o", "resolved.rs", "Path to write the resolved output")
	resolveCmd.Flags().StringVar(&cfg.GeminiAPIKey, "api-key", "", "Gemini API Key (optional, defaults to .env)")

	// 4. Attach subcommand to root and execute
	rootCmd.AddCommand(resolveCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// attemptDeterministicMerge acts as Phase 1
func attemptDeterministicMerge(cfg Config) bool {
	return false
}
