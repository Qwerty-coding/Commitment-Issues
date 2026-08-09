// setupCmd defines the placeholder CLI command for registering the tool as a Git merge driver.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// setupCmd is a placeholder for future Git merge-driver registration.
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Register the tool as a Git merge driver",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Git merge-driver setup is not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
