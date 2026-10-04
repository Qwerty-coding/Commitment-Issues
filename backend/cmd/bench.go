package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"CommitIssues/internal/bench"

	"github.com/spf13/cobra"
)

var (
	benchOut    string
	benchJSON   string
	benchRuns   int
	benchStdout bool
)

var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Run the metrics harness (JSON-vs-TOON, cache, README, budget, reuse)",
	Long: `Run the metrics harness and write the results to stdout, a markdown
report (default docs/metrics.md) and an optional JSON file. The report records
the Go version, OS/architecture and git SHA so numbers stay comparable.

Reproduce: go run . bench`,
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := bench.Run(bench.Config{
			Runs:        benchRuns,
			MarkdownOut: benchOut,
			JSONOut:     benchJSON,
			Stdout:      benchStdout || benchOut == "",
		})
		if err != nil {
			return err
		}
		if benchOut != "" && !benchStdout {
			fmt.Printf("metrics written to %s at %s\n", benchOut, time.Now().UTC().Format(time.RFC3339))
		}
		if benchJSON != "" {
			fmt.Printf("raw report written to %s\n", filepath.Clean(benchJSON))
		}
		_ = report
		return nil
	},
}

func init() {
	benchCmd.Flags().StringVar(&benchOut, "out", defaultMetricsPath(), "Markdown report path (empty prints to stdout only)")
	benchCmd.Flags().StringVar(&benchJSON, "json", "", "Optional path for the machine-readable JSON report")
	benchCmd.Flags().IntVar(&benchRuns, "runs", 5, "Repetitions for latency-style metrics")
	rootCmd.AddCommand(benchCmd)
}

// defaultMetricsPath points at <repo root>/docs/metrics.md when the command
// runs from the backend module; otherwise it resolves next to the cwd.
func defaultMetricsPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "docs/metrics.md"
	}
	if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
		return filepath.Join("..", "docs", "metrics.md")
	}
	return "docs/metrics.md"
}
