// rootCmd defines the shared CLI root command for the merge resolver tool.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd is the CLI entry point.
var rootCmd = &cobra.Command{
	Use:   "commitment-issues",
	Short: "AST-powered merge resolver for multiple languages",
}

// Execute starts the CLI and exits on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}