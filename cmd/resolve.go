// resolveCmd defines an explicit CLI entry point for running the conflict-resolution workflow.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	basePath   string
	localPath  string
	remotePath string
	outputPath string
)

// resolveCmd runs the shared resolution workflow with an explicit command name.
var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Resolve a conflict using the AST-guided workflow",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🤖 Resolving conflict...")
		if err := runResolutionWorkflow(basePath, localPath, remotePath, outputPath); err != nil {
			fmt.Printf("Resolution failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Resolution complete")
	},
}

func init() {
	resolveCmd.Flags().StringVarP(&basePath, "base", "b", "", "Base file path")
	resolveCmd.Flags().StringVarP(&localPath, "local", "l", "", "Local file path")
	resolveCmd.Flags().StringVarP(&remotePath, "remote", "r", "", "Remote file path")
	resolveCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output resolved file path")

	for _, name := range []string{"base", "local", "remote", "output"} {
		resolveCmd.MarkFlagRequired(name)
	}

	rootCmd.AddCommand(resolveCmd)
}
