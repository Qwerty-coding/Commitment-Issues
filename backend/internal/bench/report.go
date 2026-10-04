package bench

import (
	"fmt"
	"strings"
)

// renderMarkdown produces the human-readable docs/metrics.md report.
func renderMarkdown(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Metrics Report\n\n")
	fmt.Fprintf(&b, "Generated: %s  |  Go: %s  |  OS/arch: %s/%s",
		r.GeneratedAt.Format("2006-01-02 15:04:05 MST"), r.GoVersion, r.OS, r.Arch)
	if r.GitSHA != "" {
		fmt.Fprintf(&b, "  |  Commit: %s", r.GitSHA)
	}
	b.WriteString("\n\n")
	b.WriteString("Reproduce: `go run . bench`\n\n")

	// ── M1 ────────────────────────────────────────────────────────────────
	b.WriteString("## M1 — Token cost: JSON vs TOON (AI-bound payloads)\n\n")
	b.WriteString("| Fixture | JSON bytes | JSON tokens | TOON bytes | TOON tokens | Savings |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|\n")
	var jsonTok, toonTok int
	for _, row := range r.M1 {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %.1f%% |\n",
			row.Name, row.JSONBytes, row.JSONTokens, row.ToonBytes, row.ToonTokens, row.SavingsPct)
		jsonTok += row.JSONTokens
		toonTok += row.ToonTokens
	}
	if jsonTok > 0 {
		fmt.Fprintf(&b, "\n**Aggregate**: %d JSON tokens -> %d TOON tokens (%.1f%% smaller) across %d payload(s).\n\n",
			jsonTok, toonTok, pct(float64(jsonTok-toonTok), float64(jsonTok)), len(r.M1))
	}

	// ── M2 ────────────────────────────────────────────────────────────────
	b.WriteString("## M2 — AST cache latency (cold vs warm)\n\n")
	fmt.Fprintf(&b, "| Metric | Cold (µs) | Warm (µs) |\n|---|---:|---:|\n")
	fmt.Fprintf(&b, "| mean | %.1f | %.1f |\n| p50 | %.1f | %.1f |\n| p95 | %.1f | %.1f |\n\n",
		r.M2.ColdMean, r.M2.WarmMean, r.M2.ColdP50, r.M2.WarmP50, r.M2.ColdP95, r.M2.WarmP95)
	fmt.Fprintf(&b, "**Speedup**: %.1fx over %d sample(s).\n\n", r.M2.Speedup, r.M2.Samples)

	// ── M3 ────────────────────────────────────────────────────────────────
	b.WriteString("## M3 — End-to-end analyze (fresh vs warm AST cache)\n\n")
	if r.M3.Skipped != "" {
		fmt.Fprintf(&b, "Skipped: %s\n\n", r.M3.Skipped)
	} else {
		b.WriteString("| Fresh cache (ms) | Warm cache (ms) | Faster |\n|---|---:|---:|\n")
		fmt.Fprintf(&b, "| %.1f | %.1f | %.1f%% |\n\n", r.M3.FreshMS, r.M3.WarmMS, r.M3.SavingsPct)
	}

	// ── M4 ────────────────────────────────────────────────────────────────
	b.WriteString("## M4 — README pre-context cost\n\n")
	b.WriteString("| Fixture | Without (tokens) | With (tokens) | Added | Share of context |\n|---|---:|---:|---:|---:|\n")
	fmt.Fprintf(&b, "| %s | %d | %d | %d | %.1f%% |\n\n",
		r.M4.Fixture, r.M4.WithoutTokens, r.M4.WithTokens, r.M4.AddedTokens, r.M4.SharePct)

	// ── M5 ────────────────────────────────────────────────────────────────
	b.WriteString("## M5 — Soft prompt budget trimming (AI_TARGET_PROMPT_TOKENS)\n\n")
	b.WriteString("| Fixture | Before (tokens) | After (tokens) | Trimmed | Functions | Variables |\n|---|---:|---:|---:|---|---|\n")
	fmt.Fprintf(&b, "| %s | %d | %d | %.1f%% | %d -> %d | %d -> %d |\n\n",
		r.M5.Fixture, r.M5.BeforeTokens, r.M5.AfterTokens, r.M5.TrimmedPct,
		r.M5.FunctionsFrom, r.M5.FunctionsTo, r.M5.VariablesFrom, r.M5.VariablesTo)

	// ── M6 ────────────────────────────────────────────────────────────────
	b.WriteString("## M6 — Suggestion reuse across consecutive generations\n\n")
	if r.M6.Skipped != "" {
		fmt.Fprintf(&b, "Skipped: %s\n\n", r.M6.Skipped)
		return b.String()
	}
	b.WriteString("| Collisions | Pass 1 reused | Pass 1 generated | Pass 2 reused | Pass 2 generated |\n|---|---:|---:|---:|---:|\n")
	fmt.Fprintf(&b, "| %d | %d | %d | %d | %d |\n", r.M6.Collisions, r.M6.Pass1Reused, r.M6.Pass1Gen, r.M6.Pass2Reused, r.M6.Pass2Gen)
	return b.String()
}
