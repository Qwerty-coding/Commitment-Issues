package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"
)

// ValidateAndWrite applies the syntax guard and writes the output
func ValidateAndWrite(resolvedCode []byte, filePath string) error {
	parser := sitter.NewParser()
	parser.SetLanguage(rust.GetLanguage())

	// Re-parse the LLM generated code
	tree, _ := parser.ParseCtx(context.Background(), nil, resolvedCode)

	// Guard: Check for Hallucinated/Broken Syntax
	if tree.RootNode().HasError() {
		log.Println("Phase 5 Guard Failed: LLM generated invalid syntax. Falling back to Git markers.")
		return writeGitMarkers(filePath)
	}

	// Success: Write clean file
	err := os.WriteFile(filePath, resolvedCode, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	return nil
}

// writeGitMarkers is the ultimate fallback if the LLM output is structurally broken
func writeGitMarkers(filePath string) error {
	// Standard Git Marker Fallback Logic
	// e.g., <<<<<<< HEAD ... ======= ... >>>>>>>
	return fmt.Errorf("conflict requires manual resolution")
}