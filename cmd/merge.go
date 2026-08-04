package cmd

import (
	"fmt"
	"os"

	"commitment-issues/internal/engine"

	"github.com/spf13/cobra"
)

var (
	baseFile   string
	localFile  string
	remoteFile string
	outputFile string
)

var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "The primary Git merge-driver endpoint",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🚀 Initializing AST-Guided Merge Pipeline...")

		localBytes, err := os.ReadFile(localFile)
		if err != nil {
			fmt.Printf("Error reading local file: %v\n", err)
			os.Exit(1)
		}
		remoteBytes, err := os.ReadFile(remoteFile)
		if err != nil {
			fmt.Printf("Error reading remote file: %v\n", err)
			os.Exit(1)
		}

		// TODO: Phase 1 (Text Merge) and Phase 2 (Gumtree Mapping) go here.
		// For now, we mock the isolation step to feed Phase 4 directly.
		op := engine.ASTOperation{
			Action:         "UPDATE",
			EnclosingBlock: "fn main()",
			LocalCode:      string(localBytes),
			RemoteCode:     string(remoteBytes),
		}

		payload := engine.ConflictPayload{
			FilePath:   localFile,
			Operations: []engine.ASTOperation{op},
		}

		// Phase 4: Gemini resolution using reduced-token YAML
		resolvedCode, err := engine.ResolveConflictWithGemini(payload, string(localBytes))
		if err != nil {
			fmt.Printf("❌ Pipeline failure in Phase 4: %v\n", err)
			os.Exit(1)
		}

		// Phase 5: Syntax Guard
		if err := engine.ValidateAndWrite(resolvedCode, outputFile); err != nil {
			fmt.Printf("🛡️ Phase 5 Guard rejected response: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("✅ Merged output successfully written to %s\n", outputFile)
	},
}

func init() {
	mergeCmd.Flags().StringVarP(&baseFile, "base", "b", "", "Path to base file")
	mergeCmd.Flags().StringVarP(&localFile, "local", "l", "", "Path to local file")
	mergeCmd.Flags().StringVarP(&remoteFile, "remote", "r", "", "Path to remote file")
	mergeCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Path to output file")

	mergeCmd.MarkFlagRequired("base")
	mergeCmd.MarkFlagRequired("local")
	mergeCmd.MarkFlagRequired("remote")
	mergeCmd.MarkFlagRequired("output")

	rootCmd.AddCommand(mergeCmd)
}
