// mergeCmd defines the CLI workflow for resolving conflicts from base, local, and remote files.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	baseFile   string
	localFile  string
	remoteFile string
	outputFile string
)

// mergeCmd runs the shared resolution workflow for base/local/remote inputs.
var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Resolve a conflict between base, local, and remote files",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🚀 Starting merge workflow...")
		if err := runResolutionWorkflow(baseFile, localFile, remoteFile, outputFile); err != nil {
			fmt.Printf("Merge failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Merged output written to %s\n", outputFile)
	},
}

func init() {
	mergeCmd.Flags().StringVarP(&baseFile, "base", "b", "", "Path to base file")
	mergeCmd.Flags().StringVarP(&localFile, "local", "l", "", "Path to local file")
	mergeCmd.Flags().StringVarP(&remoteFile, "remote", "r", "", "Path to remote file")
	mergeCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Path to output file")

	for _, name := range []string{"base", "local", "remote", "output"} {
		mergeCmd.MarkFlagRequired(name)
	}

	rootCmd.AddCommand(mergeCmd)
}
