package main

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds the runtime configuration
type Config struct {
	BaseFilePath   string
	LocalFilePath  string
	RemoteFilePath string
	OutputFilePath string
	GeminiAPIKey   string
}

func main() {
	// Automatically load variables from .env into the system environment
	err := godotenv.Load()
	if err != nil {
		log.Println("Note: No .env file found, reading from system environment instead.")
	}

	cfg := Config{
		BaseFilePath:   "testfiles/v1.rs",
		LocalFilePath:  "testfiles/local.rs",
		RemoteFilePath: "testfiles/v2.rs",
		OutputFilePath: "testfiles/resolved.rs",
		GeminiAPIKey:   os.Getenv("GEMINI_API_KEY"),
	}

	if cfg.GeminiAPIKey == "" {
		log.Fatal("FATAL: GEMINI_API_KEY is completely empty. Please set it in your .env file.")
	}

	ctx := context.Background()
	log.Println("Starting Merge Conflict Resolver...")

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
}

func attemptDeterministicMerge(cfg Config) bool {
	return false
}
