package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"CommitIssues/internal/runstate"
)

// ─── Error propagation ───────────────────────────────────────────────────────

func TestFindConflicts_SurfacesGitFailure(t *testing.T) {
	dir := t.TempDir()
	// A directory that looks like a repository root (it has a .git entry) but is
	// not a valid Git repository, so `git ls-files -u` fails.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	conflicts, roots, err := FindConflicts(context.Background(), dir)
	if err == nil {
		t.Fatalf("expected a Git discovery failure, got conflicts=%v roots=%v", conflicts, roots)
	}
}

func TestFindConflicts_CancellationIsNeverSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	conflicts, _, err := FindConflicts(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if conflicts != nil {
		t.Fatalf("cancellation must not produce a result: %v", conflicts)
	}
}

// ─── Multi-repository isolation ──────────────────────────────────────────────

func TestProcessRepository_MultipleRepositoriesAreIsolated(t *testing.T) {
	repoA := t.TempDir()
	repoB := t.TempDir()
	writeConflictFile(t, repoA, "src/main.js", "A-ours", "A-theirs")
	writeConflictFile(t, repoB, "src/main.js", "B-ours", "B-theirs")

	run := runstate.NewRun()
	if _, err := ProcessRepository(context.Background(), run, repoA, []string{"src/main.js"}, validConfig(), false); err != nil {
		t.Fatalf("repoA: %v", err)
	}
	if _, err := ProcessRepository(context.Background(), run, repoB, []string{"src/main.js"}, validConfig(), false); err != nil {
		t.Fatalf("repoB: %v", err)
	}

	if got := len(run.AllAnalyses()); got != 2 {
		t.Fatalf("expected 2 isolated analyses for the same relative path, got %d", got)
	}
	if got := len(run.GraphKeys()); got != 2 {
		t.Fatalf("expected 2 isolated graphs for the same relative path, got %d", got)
	}

	a, ok := run.GetAnalysis(repoA, "src/main.js")
	if !ok || a.Repository != repoA {
		t.Fatalf("repoA analysis missing or mis-scoped: %+v (ok=%v)", a, ok)
	}
	b, ok := run.GetAnalysis(repoB, "src/main.js")
	if !ok || b.Repository != repoB {
		t.Fatalf("repoB analysis missing or mis-scoped: %+v (ok=%v)", b, ok)
	}

	// Graph root IDs must be repository-safe so the merged graph keeps both.
	roots := map[string]bool{}
	for _, node := range run.MergedGraph().Elements.Nodes {
		if node.Data.Kind == "file" {
			roots[node.Data.ID] = true
		}
	}
	if len(roots) != 2 {
		t.Fatalf("expected 2 distinct repository-safe graph roots, got %v", roots)
	}
}
