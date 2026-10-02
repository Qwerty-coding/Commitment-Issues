package runstate

import (
	"reflect"
	"testing"

	promptcontext "CommitIssues/internal/context"
	graph "CommitIssues/internal/graph"
	semantic "CommitIssues/internal/semantic"
)

// ─── Repository-safe keys ────────────────────────────────────────────────────

func TestFileKey_RepositoryScoped(t *testing.T) {
	if got := FileKey("repoA", "src/main.go"); got != "repoA|src/main.go" {
		t.Errorf("FileKey = %q, want repoA|src/main.go", got)
	}
	if got := FileKey("", "a.go"); got != "a.go" {
		t.Errorf("FileKey with empty repo = %q, want a.go", got)
	}
	if FileKey("repoA", "src/main.go") == FileKey("repoB", "src/main.go") {
		t.Error("keys for identical paths in different repositories must differ")
	}
}

func TestRun_IdenticalFilenamesInDifferentRepositories(t *testing.T) {
	r := NewRun()
	r.RegisterAnalysis("repoA", promptcontext.FileAnalysis{Repository: "repoA", File: "src/main.go"})
	r.RegisterAnalysis("repoB", promptcontext.FileAnalysis{Repository: "repoB", File: "src/main.go"})

	if got := len(r.AllAnalyses()); got != 2 {
		t.Fatalf("expected 2 analyses, got %d", got)
	}
	a, okA := r.GetAnalysis("repoA", "src/main.go")
	b, okB := r.GetAnalysis("repoB", "src/main.go")
	if !okA || a.Repository != "repoA" {
		t.Errorf("repoA analysis = %+v (ok=%v)", a, okA)
	}
	if !okB || b.Repository != "repoB" {
		t.Errorf("repoB analysis = %+v (ok=%v)", b, okB)
	}
}

func TestRun_PromptContextsAreRepositoryScoped(t *testing.T) {
	r := NewRun()
	r.RegisterPromptContext("repoA", "main.go", promptcontext.PromptContextIR{RepositorySummary: "A"})
	r.RegisterPromptContext("repoB", "main.go", promptcontext.PromptContextIR{RepositorySummary: "B"})

	a, okA := r.GetPromptContext("repoA", "main.go")
	b, okB := r.GetPromptContext("repoB", "main.go")
	if !okA || a.RepositorySummary != "A" {
		t.Errorf("repoA prompt = %+v (ok=%v)", a, okA)
	}
	if !okB || b.RepositorySummary != "B" {
		t.Errorf("repoB prompt = %+v (ok=%v)", b, okB)
	}
	if got := len(r.AllPromptContexts()); got != 2 {
		t.Errorf("expected 2 prompt contexts, got %d", got)
	}
}

// ─── Graph isolation ─────────────────────────────────────────────────────────

func TestRun_GraphIDsAreRepositorySafe(t *testing.T) {
	diff := semantic.SmartDiffResult{Collisions: []semantic.DiffItem{
		{Type: "COLLISION", Kind: "Function", Name: "run", Line: 1, Identity: "main.js|A|Function|run|"},
	}}
	gA := graph.BuildCyGraph("repoA", "main.js", diff)
	gB := graph.BuildCyGraph("repoB", "main.js", diff)

	r := NewRun()
	r.RegisterGraph("repoA", "main.js", gA)
	r.RegisterGraph("repoB", "main.js", gB)

	merged := r.MergedGraph()
	roots := map[string]bool{}
	for _, node := range merged.Elements.Nodes {
		if node.Data.Kind == "file" {
			roots[node.Data.ID] = true
		}
	}
	if len(roots) != 2 {
		t.Fatalf("expected 2 distinct graph roots, got %v", roots)
	}
	// Both collision nodes must survive; they must not collide by ID.
	if got := len(merged.Elements.Nodes); got != 4 {
		t.Errorf("expected 4 nodes (2 roots + 2 collisions), got %d", got)
	}
}

func TestRun_MergedGraphDeterministicAcrossRegistrationOrder(t *testing.T) {
	build := func(order []string) []string {
		r := NewRun()
		for _, repo := range order {
			r.RegisterGraph(repo, "main.go", graph.BuildCyGraph(repo, "main.go", semantic.SmartDiffResult{}))
		}
		ids := []string{}
		for _, n := range r.MergedGraph().Elements.Nodes {
			ids = append(ids, n.Data.ID)
		}
		return ids
	}

	first := build([]string{"repoB", "repoA"})
	second := build([]string{"repoA", "repoB"})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("merged graph order depends on registration order:\n%v\n%v", first, second)
	}
}

// ─── Suggestion isolation ────────────────────────────────────────────────────

func TestRun_SuggestionsAreRunAndRepositoryScoped(t *testing.T) {
	runA := NewRun()
	runB := NewRun()
	items := []Suggestion{{File: "main.go"}}

	runA.SaveSuggestions("repoA", "main.go", items)

	if _, ok := runB.GetSuggestions("repoA", "main.go"); ok {
		t.Error("suggestions leaked across runs")
	}
	if _, ok := runA.GetSuggestions("repoB", "main.go"); ok {
		t.Error("suggestions leaked across repositories")
	}
	got, ok := runA.GetSuggestions("repoA", "main.go")
	if !ok || len(got) != 1 {
		t.Fatalf("expected 1 stored suggestion, got %v (ok=%v)", got, ok)
	}
}

// ─── Repository metadata isolation ───────────────────────────────────────────

func TestRun_RepositoryMetadataIsRunScoped(t *testing.T) {
	a := NewRun()
	b := NewRun()
	a.SetRepositoryMetadata(RepositoryMetadata{Name: "A", CurrentBranch: "main"})

	if got := b.GetRepositoryMetadata(); got.Name != "" {
		t.Errorf("metadata leaked into another run: %+v", got)
	}
	if got := a.GetRepositoryMetadata(); got.Name != "A" || got.CurrentBranch != "main" {
		t.Errorf("metadata not stored: %+v", got)
	}
}
