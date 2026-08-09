// diffCmd defines the placeholder CLI command for inspecting AST-level differences.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// diffCmd is a placeholder for future AST-diff inspection.
var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Inspect AST-level differences",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("AST diff inspection is not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
}
