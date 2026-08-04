package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Structural AST diff viewer",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("📊 Displaying AST node diffs...")
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
}
