// Package bench implements the metrics harness: before/after numbers for the
// token-cost (JSON vs TOON), cache, README pre-context, prompt-budget and
// suggestion-reuse work. Results go to stdout, a markdown report and an
// optional JSON file so numbers are comparable across machines and commits.
package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	ai "CommitIssues/internal/ai"
	promptcontext "CommitIssues/internal/context"
	"CommitIssues/internal/engine"
	"CommitIssues/internal/eval"
	parser "CommitIssues/internal/parser"
	prompt "CommitIssues/internal/prompt"
	"CommitIssues/internal/runstate"
	semantic "CommitIssues/internal/semantic"
	suggestions "CommitIssues/internal/suggestions"
)

// Config controls one harness run.
type Config struct {
	// Runs is the repetition count for latency-style metrics (M2/M3).
	Runs int
	// MarkdownOut is the path of the generated markdown report ("" skips).
	MarkdownOut string
	// JSONOut is the path of the generated machine-readable report ("" skips).
	JSONOut string
	// Stdout mirrors the markdown report to stdout.
	Stdout bool
}

// Report aggregates every metric section.
type Report struct {
	GoVersion   string    `json:"goVersion"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	GitSHA      string    `json:"gitSha,omitempty"`
	GeneratedAt time.Time `json:"generatedAt"`

	M1 []M1Row  `json:"m1TokenCost"`
	M2 M2Cache  `json:"m2CacheLatency"`
	M3 M3E2E    `json:"m3E2EAnalyze"`
	M4 M4Readme `json:"m4ReadmeCost"`
	M5 M5Budget `json:"m5BudgetTrimming"`
	M6 M6Reuse  `json:"m6SuggestionReuse"`
}

// M1Row is one fixture's JSON-vs-TOON encoding cost for the AI payload.
type M1Row struct {
	Name       string  `json:"name"`
	JSONBytes  int     `json:"jsonBytes"`
	JSONTokens int     `json:"jsonTokens"`
	ToonBytes  int     `json:"toonBytes"`
	ToonTokens int     `json:"toonTokens"`
	SavingsPct float64 `json:"savingsPct"`
} // M2Cache reports cold vs warm AST-cache parse latency in microseconds.
type M2Cache struct {
	Samples  int     `json:"samples"`
	ColdMean float64 `json:"coldMeanUs"`
	ColdP50  float64 `json:"coldP50Us"`
	ColdP95  float64 `json:"coldP95Us"`
	WarmMean float64 `json:"warmMeanUs"`
	WarmP50  float64 `json:"warmP50Us"`
	WarmP95  float64 `json:"warmP95Us"`
	Speedup  float64 `json:"speedup"`
}

// M3E2E reports end-to-end ProcessRepository wall time with a fresh vs a warm cache.
type M3E2E struct {
	FreshMS    float64 `json:"freshMs"`
	WarmMS     float64 `json:"warmMs"`
	SavingsPct float64 `json:"savingsPct"`
	Skipped    string  `json:"skipped,omitempty"`
}

// M4Readme reports the token cost of including the README pre-context.
type M4Readme struct {
	Fixture       string  `json:"fixture"`
	WithoutTokens int     `json:"withoutTokens"`
	WithTokens    int     `json:"withTokens"`
	AddedTokens   int     `json:"addedTokens"`
	SharePct      float64 `json:"sharePct"`
}

// M5Budget reports the soft prompt-budget trim on the largest fixture.
type M5Budget struct {
	Fixture       string  `json:"fixture"`
	BeforeTokens  int     `json:"beforeTokens"`
	AfterTokens   int     `json:"afterTokens"`
	TrimmedPct    float64 `json:"trimmedPct"`
	FunctionsFrom int     `json:"functionsFrom"`
	FunctionsTo   int     `json:"functionsTo"`
	VariablesFrom int     `json:"variablesFrom"`
	VariablesTo   int     `json:"variablesTo"`
}

// M6Reuse reports suggestion reuse across two consecutive generation passes.
type M6Reuse struct {
	Collisions  int    `json:"collisions"`
	Pass1Reused int    `json:"pass1Reused"`
	Pass1Gen    int    `json:"pass1Generated"`
	Pass2Reused int    `json:"pass2Reused"`
	Pass2Gen    int    `json:"pass2Generated"`
	Skipped     string `json:"skipped,omitempty"`
}

// Run executes the harness and returns the report.
func Run(cfg Config) (*Report, error) {
	if cfg.Runs <= 0 {
		cfg.Runs = 5
	}
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}

	report := &Report{
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		GeneratedAt: time.Now().UTC(),
	}
	report.GitSHA = gitSHA(root)

	fixtures, err := eval.LoadFixtures(filepath.Join(root, "conflicts", "fixtures", "fixtures.json"))
	if err != nil {
		return nil, fmt.Errorf("load fixtures: %w", err)
	}

	dataset, err := buildDataset(fixtures, root)
	if err != nil {
		return nil, err
	}

	report.M1 = measureM1(dataset)
	report.M2 = measureM2(dataset, cfg.Runs)
	report.M3 = measureM3(root, cfg.Runs)
	report.M4 = measureM4(dataset, root)
	report.M5 = measureM5(dataset)
	report.M6 = measureM6(dataset)

	if cfg.MarkdownOut != "" {
		md := renderMarkdown(report)
		if err := os.MkdirAll(filepath.Dir(cfg.MarkdownOut), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(cfg.MarkdownOut, []byte(md), 0o644); err != nil {
			return nil, fmt.Errorf("write markdown: %w", err)
		}
		if cfg.Stdout {
			fmt.Print(md)
		}
	}
	if cfg.JSONOut != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(cfg.JSONOut, data, 0o644); err != nil {
			return nil, fmt.Errorf("write json: %w", err)
		}
	}
	return report, nil
}

// payload bundles one fixture's parsed versions and assembled AI payload.
type payload struct {
	Name      string
	Base      []byte
	Ours      []byte
	Theirs    []byte
	File      string
	BaseAST   parser.ASTContext
	OursAST   parser.ASTContext
	TheirsAST parser.ASTContext
	Diff      semantic.SmartDiffResult
}

func buildDataset(fixtures []eval.Fixture, root string) ([]payload, error) {
	var out []payload
	for _, f := range fixtures {
		p := payload{Name: f.File, File: f.File, Base: []byte(f.Base), Ours: []byte(f.Ours), Theirs: []byte(f.Theirs)}
		if err := fill(&p); err != nil {
			return nil, fmt.Errorf("%s: %w", f.File, err)
		}
		out = append(out, p)
	}

	// The two hand-written conflict samples round out the dataset.
	samplesDir := filepath.Join(root, "conflicts")
	entries, _ := os.ReadDir(samplesDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if parser.GetLanguageName(name) == "" || ext == ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(samplesDir, name))
		if err != nil {
			continue
		}
		p := payload{Name: "conflicts/" + name, File: name, Base: data, Ours: data, Theirs: data}
		if err := fill(&p); err != nil {
			continue // conflict markers are not parseable; skip quietly
		}
		out = append(out, p)
	}
	return out, nil
}

func fill(p *payload) error {
	var err error
	if p.BaseAST, err = parser.ParseAndExtract(p.File, p.Base); err != nil {
		return err
	}
	if p.OursAST, err = parser.ParseAndExtract(p.File, p.Ours); err != nil {
		return err
	}
	if p.TheirsAST, err = parser.ParseAndExtract(p.File, p.Theirs); err != nil {
		return err
	}
	p.Diff = semantic.GenerateSmartDiff(context.Background(), p.BaseAST, p.OursAST, p.TheirsAST)
	return nil
}

func (p payload) aiPayload() prompt.AIRequestPayload {
	return prompt.AIRequestPayload{
		FileName:     p.File,
		BaseCode:     string(p.Base),
		OurCode:      string(p.Ours),
		TheirCode:    string(p.Theirs),
		OurASTData:   p.OursAST,
		TheirASTData: p.TheirsAST,
		SmartDiff:    p.Diff,
	}
}

// ── M1: token cost JSON vs TOON ─────────────────────────────────────────────

func measureM1(dataset []payload) []M1Row {
	rows := make([]M1Row, 0, len(dataset))
	for _, p := range dataset {
		payload := p.aiPayload()
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		toonBytes, err := prompt.MarshalAIRequestPayload(payload)
		if err != nil {
			continue
		}
		row := M1Row{
			Name:       p.Name,
			JSONBytes:  len(jsonBytes),
			JSONTokens: promptcontext.EstimateTokens(string(jsonBytes)),
			ToonBytes:  len(toonBytes),
			ToonTokens: promptcontext.EstimateTokens(string(toonBytes)),
		}
		if row.JSONTokens > 0 {
			row.SavingsPct = pct(float64(row.JSONTokens-row.ToonTokens), float64(row.JSONTokens))
		}
		rows = append(rows, row)
	}
	return rows
}

// ── M2: AST cache latency ───────────────────────────────────────────────────

// measureM2 times whole batches rather than individual ops: per-op wall time
// at microsecond scale is below the timer resolution on some platforms
// (notably Windows), which would zero out the percentiles. The warm batch is
// additionally repeated to stay above that resolution.
const warmBatchRepeat = 200

func measureM2(dataset []payload, runs int) M2Cache {
	const versionsPerPayload = 3
	opsPerBatch := len(dataset) * versionsPerPayload

	var cold, warm []float64
	for i := 0; i < runs; i++ {
		cache := engine.DefaultASTCache()

		start := time.Now()
		for _, p := range dataset {
			for _, version := range []struct {
				file   string
				source []byte
			}{{p.File, p.Base}, {p.File, p.Ours}, {p.File, p.Theirs}} {
				_, _, _ = cache.GetOrParse(context.Background(), "bench-repo", version.file, version.source)
			}
		}
		cold = append(cold, float64(time.Since(start).Nanoseconds())/1000.0/float64(opsPerBatch))

		// Same cache instance: every version is now a warm hit. Repeat the
		// batch so the measured interval dwarfs the timer resolution.
		start = time.Now()
		for repeat := 0; repeat < warmBatchRepeat; repeat++ {
			for _, p := range dataset {
				for _, version := range []struct {
					file   string
					source []byte
				}{{p.File, p.Base}, {p.File, p.Ours}, {p.File, p.Theirs}} {
					_, _, _ = cache.GetOrParse(context.Background(), "bench-repo", version.file, version.source)
				}
			}
		}
		warm = append(warm, float64(time.Since(start).Nanoseconds())/1000.0/float64(opsPerBatch*warmBatchRepeat))
	}

	out := M2Cache{Samples: runs}
	out.ColdMean = mean(cold)
	out.ColdP50 = percentile(cold, 50)
	out.ColdP95 = percentile(cold, 95)
	out.WarmMean = mean(warm)
	out.WarmP50 = percentile(warm, 50)
	out.WarmP95 = percentile(warm, 95)
	if out.WarmMean > 0 {
		out.Speedup = out.ColdMean / out.WarmMean
	}
	return out
}

// ── M3: end-to-end analyze with and without cache ───────────────────────────

func measureM3(root string, runs int) M3E2E {
	repo, cleanup, err := tempConflictedRepo()
	if err != nil {
		return M3E2E{Skipped: "temp git repo unavailable: " + err.Error()}
	}
	defer cleanup()

	conflictsByRepo, _, err := engine.FindConflicts(context.Background(), repo)
	if err != nil {
		return M3E2E{Skipped: "conflict scan failed: " + err.Error()}
	}
	var files []string
	for _, f := range conflictsByRepo {
		files = append(files, f...)
	}
	if len(files) == 0 {
		return M3E2E{Skipped: "no conflicts materialized in temp repo"}
	}

	var fresh, warm []float64
	for i := 0; i < runs; i++ {
		cfg := engine.DefaultConfig()
		cfg.ASTCache = nil // per-run fresh cache

		run1 := runstate.NewRun()
		start := time.Now()
		if _, perr := engine.ProcessRepository(context.Background(), run1, repo, files, cfg, false); perr != nil {
			return M3E2E{Skipped: "analysis failed: " + perr.Error()}
		}
		fresh = append(fresh, float64(time.Since(start).Microseconds())/1000.0)

		// Second pass reuses the same cache instance: every version is a hit.
		run2 := runstate.NewRun()
		start = time.Now()
		if _, perr := engine.ProcessRepository(context.Background(), run2, repo, files, cfg, false); perr != nil {
			return M3E2E{Skipped: "warm analysis failed: " + perr.Error()}
		}
		warm = append(warm, float64(time.Since(start).Microseconds())/1000.0)
	}

	freshMean, warmMean := mean(fresh), mean(warm)
	out := M3E2E{FreshMS: freshMean, WarmMS: warmMean}
	if freshMean > 0 {
		out.SavingsPct = pct(freshMean-warmMean, freshMean)
	}
	return out
}

// tempConflictedRepo materializes a real two-branch merge conflict.
func tempConflictedRepo() (string, func(), error) {
	dir, err := os.MkdirTemp("", "commitissues-bench-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	gitCmd := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=bench", "GIT_AUTHOR_EMAIL=bench@example.com",
			"GIT_COMMITTER_NAME=bench", "GIT_COMMITTER_EMAIL=bench@example.com")
		return cmd.Run()
	}

	base := "package demo\n\nfunc Calculate(x int) int {\n\treturn x + 1\n}\n"
	ours := "package demo\n\nfunc Calculate(x int) int {\n\treturn x + 2\n}\n"
	theirs := "package demo\n\nfunc Calculate(x int) int {\n\treturn x * 2\n}\n"

	write := func(content string) error {
		return os.WriteFile(filepath.Join(dir, "calc.go"), []byte(content), 0o644)
	}

	// Each step runs a git command and optionally writes the file first. The
	// final merge is EXPECTED to exit non-zero: it materializes the conflict.
	type step struct {
		args      []string
		write     string
		allowFail bool
	}
	steps := []step{
		{args: []string{"init", "-q"}},
		// Force the branch name regardless of the machine's init.defaultBranch.
		{args: []string{"symbolic-ref", "HEAD", "refs/heads/main"}},
		{args: []string{"add", "."}},
		{args: []string{"commit", "-qm", "base"}},
		{args: []string{"checkout", "-q", "-b", "feature"}},
		{write: theirs},
		{args: []string{"commit", "-qam", "theirs"}},
		{args: []string{"checkout", "-q", "main"}},
		{write: ours},
		{args: []string{"commit", "-qam", "ours"}},
		{args: []string{"merge", "--no-edit", "feature"}, allowFail: true},
	}
	if err := write(base); err != nil {
		cleanup()
		return "", nil, err
	}
	for _, s := range steps {
		if s.write != "" {
			if err := write(s.write); err != nil {
				cleanup()
				return "", nil, err
			}
		}
		if len(s.args) == 0 {
			continue
		}
		if err := gitCmd(s.args...); err != nil {
			if s.allowFail {
				continue
			}
			cleanup()
			return "", nil, fmt.Errorf("git %s: %w", strings.Join(s.args, " "), err)
		}
	}
	return dir, cleanup, nil
}

// ── M4: README pre-context cost ─────────────────────────────────────────────

func measureM4(dataset []payload, root string) M4Readme {
	out := M4Readme{}
	big := representative(dataset)
	if big.Name == "" {
		return out
	}
	excerpt, _, err := promptcontext.LoadReadmeContext(root, 4096)
	if err != nil {
		excerpt = "# Bench\nSynthetic project context used when the repository README is unavailable.\n"
	}
	if excerpt == "" {
		excerpt = "# Bench\nSynthetic project context used when the repository README is unavailable.\n"
	}

	files := []string{big.File}
	merged := semantic.MergeSemanticGraphs(
		semantic.BuildSemanticGraph(big.OursAST),
		semantic.BuildSemanticGraph(big.TheirsAST),
	)
	scope := semantic.ComputeConflictScope(merged, big.Diff.Collisions)

	without := promptcontext.BuildPromptContext("Repository root: bench", "", files, scope, big.OursAST, big.TheirsAST)
	with := promptcontext.BuildPromptContext("Repository root: bench", excerpt, files, scope, big.OursAST, big.TheirsAST)

	out.Fixture = big.Name
	out.WithoutTokens = without.EstimatedTokens
	out.WithTokens = with.EstimatedTokens
	out.AddedTokens = with.EstimatedTokens - without.EstimatedTokens
	if with.EstimatedTokens > 0 {
		out.SharePct = pct(float64(out.AddedTokens), float64(with.EstimatedTokens))
	}
	return out
}

// ── M5: budget trimming ─────────────────────────────────────────────────────

func measureM5(dataset []payload) M5Budget {
	big := representative(dataset)
	out := M5Budget{Fixture: big.Name}
	if big.Name == "" {
		return out
	}
	excerpt := strings.Repeat("# Project context line for budget measurement.\n", 120)
	files := []string{big.File}
	merged := semantic.MergeSemanticGraphs(
		semantic.BuildSemanticGraph(big.OursAST),
		semantic.BuildSemanticGraph(big.TheirsAST),
	)
	scope := semantic.ComputeConflictScope(merged, big.Diff.Collisions)

	before := promptcontext.BuildPromptContext("Repository root: bench", excerpt, files, scope, big.OursAST, big.TheirsAST)
	// Stress the trim order with a target at half the assembled size so the
	// README -> functions -> variables cascade actually engages.
	after := promptcontext.ApplyTargetBudget(before, before.EstimatedTokens/2)

	out.BeforeTokens = before.EstimatedTokens
	out.AfterTokens = after.EstimatedTokens
	out.FunctionsFrom = len(before.Functions)
	out.FunctionsTo = len(after.Functions)
	out.VariablesFrom = len(before.Variables)
	out.VariablesTo = len(after.Variables)
	if before.EstimatedTokens > 0 {
		out.TrimmedPct = pct(float64(before.EstimatedTokens-after.EstimatedTokens), float64(before.EstimatedTokens))
	}
	return out
}

// ── M6: suggestion reuse ────────────────────────────────────────────────────

func measureM6(dataset []payload) M6Reuse {
	big := largest(dataset)
	if big.Name == "" || len(big.Diff.Collisions) == 0 {
		return M6Reuse{Skipped: "no collisions available"}
	}

	// Local fake OpenAI-compatible provider: no external AI involved. The
	// health check needs /api/tags; generation returns a valid resolution.
	model := "bench-model"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/api/tags") {
			_, _ = w.Write([]byte(`{"models":[{"name":"` + model + `:latest"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"explanation\":\"bench\",\"suggested_code\":\"return 0\",\"confidence_score\":95}"}}]}`))
	}))
	defer srv.Close()

	run := runstate.NewRun()
	run.RegisterAnalysis("bench-repo", promptcontext.FileAnalysis{
		Repository: "bench-repo",
		File:       big.File,
		SmartDiff:  big.Diff,
	})
	// Seed every collision as an already-complete suggestion.
	for _, collision := range big.Diff.Collisions {
		run.SaveSuggestion("bench-repo", runstate.Suggestion{
			ID:           runstate.SuggestionKey("bench-repo", big.File, semantic.DiffKey(collision)),
			File:         big.File,
			Repository:   "bench-repo",
			Collision:    collision,
			CollisionKey: semantic.DiffKey(collision),
			Status:       runstate.StatusComplete,
			Revision:     1,
		})
	}

	cfg := ai.Default("ollama")
	cfg.Model = model
	cfg.BaseURL = srv.URL
	cfg.RequestTimeout = 5 * time.Second
	generator := &suggestions.Generator{Cfg: cfg, MaxConcurrency: 2}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := M6Reuse{Collisions: len(big.Diff.Collisions)}
	res1, err := generator.GenerateForRun(ctx, run)
	if err != nil {
		return M6Reuse{Collisions: len(big.Diff.Collisions), Skipped: "generation failed: " + err.Error()}
	}
	out.Pass1Reused = res1.Meta.Reused
	out.Pass1Gen = res1.Meta.Generated

	res2, err := generator.GenerateForRun(ctx, run)
	if err != nil {
		return out
	}
	out.Pass2Reused = res2.Meta.Reused
	out.Pass2Gen = res2.Meta.Generated
	return out
}

// ── helpers ─────────────────────────────────────────────────────────────────

// representative picks the fixture with the richest ACTUAL prompt context
// (functions + variables in scope) for the context-shape metrics (M4/M5). The
// raw conflicts/ samples are excluded: their ASTs come from marker-laden text,
// which yields noise symbols. Class/method-only collisions (Java/C#) carry no
// Function-kind scope symbols, so scoring uses the assembled context.
func representative(dataset []payload) payload {
	best := payload{}
	bestScore := -1
	for _, p := range dataset {
		if strings.HasPrefix(p.Name, "conflicts/") {
			continue
		}
		if len(p.Diff.Collisions) == 0 {
			continue
		}
		merged := semantic.MergeSemanticGraphs(
			semantic.BuildSemanticGraph(p.OursAST),
			semantic.BuildSemanticGraph(p.TheirsAST),
		)
		scope := semantic.ComputeConflictScope(merged, p.Diff.Collisions)
		ir := promptcontext.BuildPromptContext("Repository root: bench", "", []string{p.File}, scope, p.OursAST, p.TheirsAST)
		score := len(ir.Functions)*100 + len(ir.Variables)*50 + ir.EstimatedTokens
		if score > bestScore {
			bestScore = score
			best = p
		}
	}
	return best
}

// largest picks the payload with the most collisions (tie-break: encoded
// size) so metrics M4-M6 exercise real conflict content, not collision-free
// samples.
func largest(dataset []payload) payload {
	best := payload{}
	score := -1
	for _, p := range dataset {
		encoded, err := prompt.MarshalAIRequestPayload(p.aiPayload())
		if err != nil {
			continue
		}
		s := len(p.Diff.Collisions)*1_000_000 + len(encoded)
		if s > score {
			score = s
			best = p
		}
	}
	return best
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	idx := int((p / 100.0) * float64(len(sorted)-1))
	return sorted[idx]
}

func pct(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return part / whole * 100
}

func gitSHA(root string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			parent := filepath.Dir(dir)
			if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
				return parent, nil
			}
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate the backend module root")
		}
		dir = parent
	}
}
