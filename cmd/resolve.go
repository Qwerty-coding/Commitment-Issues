package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"commitment-issues/internal/engine"

	"github.com/spf13/cobra"
)

var (
	basePath   string
	localPath  string
	remotePath string
	outputPath string
)

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Interactive post-conflict recovery tool",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🤖 Starting interactive terminal resolution...")

		localCodeBytes, err := os.ReadFile(localPath)
		if err != nil {
			log.Fatalf("❌ Failed to read local file: %v", err)
		}

		payload, err := engine.BuildASTPayload(basePath, localPath, remotePath)
		if err != nil {
			log.Fatalf("❌ Failed to build AST diff: %v", err)
		}

		fmt.Println("🧠 Sending AST diff to Gemini...")
		resolvedCode, err := engine.ResolveConflictWithGemini(payload, string(localCodeBytes))
		if err != nil {
			log.Fatalf("❌ Gemini resolution failed: %v", err)
		}

		if strings.TrimSpace(resolvedCode) == "" {
			log.Fatalf("❌ Gemini returned empty resolved code")
		}

		if err := engine.ValidateAndWrite(resolvedCode, outputPath); err != nil {
			log.Fatalf("❌ Failed to write validated output: %v", err)
		}

		fmt.Println("✅ Resolution complete!")
	},
}

func init() {
	resolveCmd.Flags().StringVarP(&basePath, "base", "b", "", "Base file path")
	resolveCmd.Flags().StringVarP(&localPath, "local", "l", "", "Local file path")
	resolveCmd.Flags().StringVarP(&remotePath, "remote", "r", "", "Remote file path")
	resolveCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output resolved file path")

	resolveCmd.MarkFlagRequired("base")
	resolveCmd.MarkFlagRequired("local")
	resolveCmd.MarkFlagRequired("remote")
	resolveCmd.MarkFlagRequired("output")

	rootCmd.AddCommand(resolveCmd)
}
