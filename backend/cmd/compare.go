package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"CommitIssues/internal/compare"
	"github.com/spf13/cobra"
)

var (
	compareRepo      string
	compareOtherRepo string
	compareBase      string
	compareOurs      string
	compareTheirs    string
	compareJSON      bool
)

var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "Compare commits or repositories to detect hypothetical merge conflicts",
	Long: `Compare two Git commits or branches without mutating the working tree.
Automatically resolves the merge base, extracts file trees, detects additions,
deletions, renames, and binary files, and executes AST semantic collision analysis.

Examples:
  CommitIssues compare --repo . --ours feature-a --theirs feature-b
  CommitIssues compare --repo . --base main --ours branch-a --theirs branch-b
  CommitIssues compare --repo ./repoA --other-repo ./repoB --ours main --theirs main --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if compareOurs == "" || compareTheirs == "" {
			return fmt.Errorf("both --ours and --theirs flags are required")
		}

		opts := compare.Options{
			Repo1:     compareRepo,
			Repo2:     compareOtherRepo,
			BaseRef:   compareBase,
			OursRef:   compareOurs,
			TheirsRef: compareTheirs,
		}

		result, err := compare.CompareCommits(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if compareJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		// Human-readable summary output
		fmt.Printf("=== Hypothetical Merge Comparison ===\n")
		fmt.Printf("Repository: %s\n", result.Repo1)
		if result.Repo2 != "" {
			fmt.Printf("Other Repository: %s\n", result.Repo2)
		}
		if result.UnrelatedRepositories {
			fmt.Printf("Status: UNRELATED REPOSITORIES (no common Git merge base; full tree comparison)\n")
		} else {
			fmt.Printf("Merge Base: %s\n", result.BaseRef)
		}
		fmt.Printf("Ours: %s | Theirs: %s\n\n", result.OursRef, result.TheirsRef)

		fmt.Printf("Summary:\n")
		fmt.Printf("  Total Changed Files:     %d\n", result.Summary.TotalFiles)
		fmt.Printf("  Ours-Only Changes:       %d\n", result.Summary.OursOnly)
		fmt.Printf("  Theirs-Only Changes:     %d\n", result.Summary.TheirsOnly)
		fmt.Printf("  Identical Changes:       %d\n", result.Summary.Identical)
		fmt.Printf("  Structural Collisions:   %d\n", result.Summary.StructuralCollisions)
		fmt.Printf("  Content Conflicts:       %d\n", result.Summary.ContentConflicts)
		fmt.Printf("  Add/Add Conflicts:       %d\n", result.Summary.AddAddConflicts)
		fmt.Printf("  Delete/Modify Conflicts: %d\n", result.Summary.DeleteModifyConflicts)
		fmt.Printf("  Rename Conflicts:        %d\n", result.Summary.RenameConflicts)
		fmt.Printf("  Binary Conflicts:        %d\n", result.Summary.BinaryConflicts)
		fmt.Printf("  Unsupported Languages:   %d\n", result.Summary.Unsupported)
		fmt.Printf("  Analysis Errors:         %d\n\n", result.Summary.Errors)

		if len(result.Files) > 0 {
			fmt.Printf("File Details:\n")
			for _, f := range result.Files {
				fmt.Printf("  [%s] %s\n", f.Status, f.Path)
				fmt.Printf("    Explanation:    %s\n", f.Explanation)
				fmt.Printf("    Recommendation: %s\n", f.Recommendation)
			}
		}

		return nil
	},
}

func init() {
	compareCmd.Flags().StringVar(&compareRepo, "repo", ".", "Path to repository (default: current directory)")
	compareCmd.Flags().StringVar(&compareOtherRepo, "other-repo", "", "Path to second repository (for 2-repo mode)")
	compareCmd.Flags().StringVar(&compareBase, "base", "", "Base commit or branch (optional; auto-detected if omitted)")
	compareCmd.Flags().StringVar(&compareOurs, "ours", "", "Ours commit or branch")
	compareCmd.Flags().StringVar(&compareTheirs, "theirs", "", "Theirs commit or branch")
	compareCmd.Flags().BoolVar(&compareJSON, "json", false, "Output results in JSON format")

	rootCmd.AddCommand(compareCmd)
}
