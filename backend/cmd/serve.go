package cmd

import (
	"fmt"

	"CommitIssues/internal/api"
	"github.com/spf13/cobra"
)

var port string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the interactive AST dependency graph visualization server",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("🚀 Starting Graph Server on http://localhost%s\n", port)
		api.StartGraphServer(port)
	},
}

func init() {
	serveCmd.Flags().StringVarP(&port, "port", "p", ":8080", "Port to serve graph web UI")
	rootCmd.AddCommand(serveCmd)
}