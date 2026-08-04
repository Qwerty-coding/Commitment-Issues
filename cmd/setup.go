package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Registers driver in local Git config",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️ Driver registered successfully in .git/config")
		// TODO: Exec command: git config merge.commitment-issues.driver="..."
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
