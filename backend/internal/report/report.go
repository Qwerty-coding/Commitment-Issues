package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	parser "CommitIssues/internal/parser"
	prompt "CommitIssues/internal/prompt"
	semantic "CommitIssues/internal/semantic"
)

func PrintASTContext(w io.Writer, label string, data parser.ASTContext) {
	fmt.Fprintf(w, "\n--- %s AST DATA ---\n", label)

	fmt.Fprintln(w, "Functions:")
	if len(data.Functions) == 0 {
		fmt.Fprintln(w, "  - (none)")
	} else {
		for _, fn := range data.Functions {
			fmt.Fprintf(w, "  - %s (Line %d)\n", fn.Name, fn.Line)
		}
	}

	fmt.Fprintln(w, "Variables:")
	if len(data.Variables) == 0 {
		fmt.Fprintln(w, "  - (none)")
	} else {
		for _, v := range data.Variables {
			fmt.Fprintf(w, "  - %s (Line %d)\n", v.Name, v.Line)
		}
	}
}

func PrintPayloadJSON(w io.Writer, payload prompt.AIRequestPayload, jsonBytes []byte) {
	fmt.Fprintln(w, "\n--- AI REQUEST PAYLOAD (JSON) ---")
	fmt.Fprintln(w, string(jsonBytes))
}

func SaveReportFile(jsonBytes []byte, repoRoot, fileName string) (string, error) {
	dir := "reports"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	safeName := SanitizeForFilename(repoRoot) + "__" + SanitizeForFilename(fileName) + ".report.json"
	path := filepath.Join(dir, safeName)
	if err := os.WriteFile(path, jsonBytes, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func SanitizeForFilename(s string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", ".", "_")
	return replacer.Replace(s)
}

func SummarizeContent(content string) string {
	if content == "" {
		return "(empty)"
	}
	flat := strings.Join(strings.Fields(content), " ")
	const maxLen = 100
	if len(flat) > maxLen {
		return flat[:maxLen] + "..."
	}
	return flat
}

func PrintDiffReport(w io.Writer, result semantic.SmartDiffResult) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintln(w, "SMART 3-WAY STRUCTURAL ANALYSIS")
	fmt.Fprintf(w, "Collisions: %d | Our changes: %d | Their changes: %d\n",
		len(result.Collisions), len(result.OurChanges), len(result.TheirChanges))
	fmt.Fprintln(w, strings.Repeat("=", 80))

	PrintCollisionSection(w, result.Collisions)
	PrintChangeSection(w, "OUR CHANGES (Safe to Apply)", result.OurChanges)
	PrintChangeSection(w, "THEIR CHANGES (Safe to Apply)", result.TheirChanges)
}

func PrintCollisionSection(w io.Writer, items []semantic.DiffItem) {
	if len(items) == 0 {
		fmt.Fprintln(w, "\nNo collisions found.")
		return
	}

	fmt.Fprintln(w, "\nCOLLISIONS (Need AI or manual review)")
	for i, c := range items {
		fmt.Fprintf(w, "\n[%d] %s - %s (Line %d)\n", i+1, c.Kind, c.Name, c.Line)
		fmt.Fprintf(w, "      Base   : %s\n", SummarizeContent(c.BaseContent))
		fmt.Fprintf(w, "      Ours   : %s\n", SummarizeContent(c.OurContent))
		fmt.Fprintf(w, "      Theirs : %s\n", SummarizeContent(c.TheirContent))
	}
}

func PrintChangeSection(w io.Writer, title string, items []semantic.DiffItem) {
	fmt.Fprintf(w, "\n%s\n", title)
	if len(items) == 0 {
		fmt.Fprintln(w, "  - (none)")
		return
	}

	for i, c := range items {
		fmt.Fprintf(w, "\n[%d] %s - %s (Line %d)\n", i+1, c.Kind, c.Name, c.Line)
		newContent := c.OurContent
		if title == "THEIR CHANGES (Safe to Apply)" {
			newContent = c.TheirContent
		}
		switch c.Type {
		case "ADDED":
			fmt.Fprintf(w, "      New content : %s\n", SummarizeContent(newContent))
		case "UPDATED":
			fmt.Fprintf(w, "      Before : %s\n", SummarizeContent(c.BaseContent))
			fmt.Fprintf(w, "      After  : %s\n", SummarizeContent(newContent))
		case "DELETED":
			fmt.Fprintf(w, "      Removed content : %s\n", SummarizeContent(c.BaseContent))
		}
	}
}
