package runstate

import (
	"fmt"
	"sync"
	"testing"

	promptcontext "CommitIssues/internal/context"
	graph "CommitIssues/internal/graph"
)

func cyGraph(file, nodeID string) graph.CyGraph {
	return graph.CyGraph{Elements: graph.CyElements{
		Nodes: []graph.CyNode{
			{Data: graph.CyNodeData{ID: "file__" + file, Kind: "file"}},
			{Data: graph.CyNodeData{ID: nodeID, Kind: "Function"}},
		},
		Edges: []graph.CyEdge{},
	}}
}

func TestNewRun_UniqueIDs(t *testing.T) {
	a := NewRun()
	b := NewRun()
	if a.ID == "" || b.ID == "" {
		t.Fatal("run IDs must not be empty")
	}
	if a.ID == b.ID {
		t.Fatal("run IDs must be unique")
	}
}

func TestRun_NewRunWithID(t *testing.T) {
	r := NewRunWithID("run-42")
	if r.ID != "run-42" {
		t.Errorf("ID = %q, want run-42", r.ID)
	}
	if empty := NewRunWithID(""); empty.ID == "" {
		t.Error("empty ID should be replaced with a generated one")
	}
}

func TestRun_RepeatedAnalysisRegistrationDoesNotDuplicate(t *testing.T) {
	r := NewRun()
	r.RegisterAnalysis(promptcontext.FileAnalysis{File: "a.go"})
	r.RegisterAnalysis(promptcontext.FileAnalysis{File: "a.go"})
	r.RegisterAnalysis(promptcontext.FileAnalysis{File: "a.go"})

	if got := len(r.AllAnalyses()); got != 1 {
		t.Fatalf("expected 1 analysis after repeated registration, got %d", got)
	}
}

func TestRun_AnalysesAreSortedByFile(t *testing.T) {
	r := NewRun()
	for _, file := range []string{"c.go", "a.go", "b.go"} {
		r.RegisterAnalysis(promptcontext.FileAnalysis{File: file})
	}
	got := r.AllAnalyses()
	want := []string{"a.go", "b.go", "c.go"}
	for i, file := range want {
		if got[i].File != file {
			t.Errorf("index %d = %q, want %q", i, got[i].File, file)
		}
	}
}

func TestRun_RepeatedGraphScanDoesNotDuplicateNodes(t *testing.T) {
	r := NewRun()
	// The same file is scanned three times (e.g. three runs in one process).
	r.RegisterGraph("app.js", cyGraph("app.js", "fn__login"))
	r.RegisterGraph("app.js", cyGraph("app.js", "fn__login"))
	r.RegisterGraph("app.js", cyGraph("app.js", "fn__login"))

	merged := r.MergedGraph()
	if len(merged.Elements.Nodes) != 2 {
		t.Fatalf("expected 2 unique nodes after repeated scans, got %d", len(merged.Elements.Nodes))
	}

	seen := map[string]int{}
	for _, n := range merged.Elements.Nodes {
		seen[n.Data.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("node %q duplicated %d times", id, count)
		}
	}
}

func TestRun_MergedGraphIsSorted(t *testing.T) {
	r := NewRun()
	r.RegisterGraph("z.js", cyGraph("z.js", "fn__z"))
	r.RegisterGraph("a.js", cyGraph("a.js", "fn__a"))

	ids := []string{}
	for _, n := range r.MergedGraph().Elements.Nodes {
		ids = append(ids, n.Data.ID)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatalf("merged node IDs not sorted: %v", ids)
		}
	}
}

func TestRun_RunsAreIsolated(t *testing.T) {
	first := NewRun()
	second := NewRun()

	first.RegisterAnalysis(promptcontext.FileAnalysis{File: "only-first.go"})
	first.RegisterGraph("only-first.js", cyGraph("only-first.js", "fn__first"))

	if _, ok := second.GetAnalysis("only-first.go"); ok {
		t.Error("analysis leaked from first run into second run")
	}
	if len(second.AllAnalyses()) != 0 {
		t.Errorf("second run should have no analyses, got %d", len(second.AllAnalyses()))
	}
	if len(second.MergedGraph().Elements.Nodes) != 0 {
		t.Error("graph state leaked between runs")
	}
	if files := second.GraphFiles(); len(files) != 0 {
		t.Errorf("second run should have no graph files, got %v", files)
	}
}

func TestRun_ReportsAreScopedAndDeletable(t *testing.T) {
	r := NewRun()
	key := r.SaveReport("/repo", "a.go", []byte("report"))
	if key == "" {
		t.Fatal("expected a cache key")
	}
	data, ok := r.GetReport("/repo", "a.go")
	if !ok || string(data) != "report" {
		t.Fatalf("report not stored correctly: %q %v", data, ok)
	}
	if _, ok := r.GetReport("/other", "a.go"); ok {
		t.Error("report should be scoped per repository")
	}
	r.DeleteReport("/repo", "a.go")
	if _, ok := r.GetReport("/repo", "a.go"); ok {
		t.Error("report should be deleted")
	}
}

func TestRun_ConcurrentRegistrationIsRaceSafe(t *testing.T) {
	r := NewRun()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			file := fmt.Sprintf("file%02d.go", i%10)
			r.RegisterAnalysis(promptcontext.FileAnalysis{File: file})
			r.RegisterGraph(file, cyGraph(file, fmt.Sprintf("fn__%d", i%10)))
		}(i)
	}
	wg.Wait()

	if got := len(r.AllAnalyses()); got != 10 {
		t.Errorf("expected 10 unique analyses, got %d", got)
	}
	if got := len(r.GraphFiles()); got != 10 {
		t.Errorf("expected 10 unique graph files, got %d", got)
	}
}
