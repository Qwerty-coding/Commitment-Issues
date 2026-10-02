package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "CommitIssues",
	Short: "CommitIssues: AST-driven Git merge conflict analyzer and resolver",
	// Errors from subcommands are structured and deterministic; suppressing
	// usage output keeps CLI failures readable and machine-parseable.
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
