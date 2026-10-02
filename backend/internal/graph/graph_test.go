package graph

import (
	"reflect"
	"testing"

	semantic "CommitIssues/internal/semantic"
)

func sampleDiff() semantic.SmartDiffResult {
	return semantic.SmartDiffResult{
		Collisions: []semantic.DiffItem{
			{Type: "COLLISION", Kind: "Function", Name: "zebra", Line: 10, Identity: "Function:zebra"},
			{Type: "COLLISION", Kind: "Function", Name: "alpha", Line: 1, Identity: "Function:alpha"},
		},
		OurChanges: []semantic.DiffItem{
			{Type: "ADDED", Kind: "Variable", Name: "beta", Line: 5, Identity: "Variable:beta"},
		},
		TheirChanges: []semantic.DiffItem{
			{Type: "DELETED", Kind: "Function", Name: "gamma", Line: 8, Identity: "Function:gamma"},
		},
	}
}

func nodeIDsEquivalent(g CyGraph) []string {
	ids := make([]string, 0, len(g.Elements.Nodes))
	for _, n := range g.Elements.Nodes {
		ids = append(ids, n.Data.ID)
	}
	return ids
}

func TestBuildCyGraph_DeterministicOrdering(t *testing.T) {
	first := BuildCyGraph("", "app.js", sampleDiff())
	second := BuildCyGraph("", "app.js", sampleDiff())

	if !reflect.DeepEqual(nodeIDsEquivalent(first), nodeIDsEquivalent(second)) {
		t.Fatalf("node order is not deterministic:\n%v\n%v", nodeIDsEquivalent(first), nodeIDsEquivalent(second))
	}
	if !reflect.DeepEqual(first.Elements.Edges, second.Elements.Edges) {
		t.Fatalf("edge order is not deterministic")
	}
}

func TestBuildCyGraph_NoDuplicateNodes(t *testing.T) {
	g := BuildCyGraph("", "app.js", sampleDiff())

	seen := map[string]int{}
	for _, n := range g.Elements.Nodes {
		seen[n.Data.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate node %q appears %d times", id, count)
		}
	}
	seenEdges := map[string]int{}
	for _, e := range g.Elements.Edges {
		seenEdges[e.Data.ID]++
	}
	for id, count := range seenEdges {
		if count > 1 {
			t.Errorf("duplicate edge %q appears %d times", id, count)
		}
	}
}

func TestBuildCyGraph_RootNodeFirst(t *testing.T) {
	g := BuildCyGraph("", "app.js", sampleDiff())
	if len(g.Elements.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
	if g.Elements.Nodes[0].Data.Kind != "file" {
		t.Errorf("first node should be the file root, got %q", g.Elements.Nodes[0].Data.Kind)
	}
}

func TestBuildCyGraph_StableAcrossRepeatedScans(t *testing.T) {
	// Simulate a repeated scan producing the same graph twice.
	first := BuildCyGraph("", "app.js", sampleDiff())
	second := BuildCyGraph("", "app.js", sampleDiff())
	merged := MergeGraphs([]CyGraph{first, second})

	if len(merged.Elements.Nodes) != len(first.Elements.Nodes) {
		t.Fatalf("merged graph duplicated nodes: %d vs %d", len(merged.Elements.Nodes), len(first.Elements.Nodes))
	}
	if len(merged.Elements.Edges) != len(first.Elements.Edges) {
		t.Fatalf("merged graph duplicated edges: %d vs %d", len(merged.Elements.Edges), len(first.Elements.Edges))
	}
}

func TestMergeGraphs_SortsByID(t *testing.T) {
	g1 := BuildCyGraph("", "z.js", sampleDiff())
	g2 := BuildCyGraph("", "a.js", sampleDiff())
	merged := MergeGraphs([]CyGraph{g1, g2})

	ids := nodeIDsEquivalent(merged)
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatalf("merged node IDs not sorted: %v", ids)
		}
	}
}

func TestMergeGraphs_Empty(t *testing.T) {
	merged := MergeGraphs(nil)
	if len(merged.Elements.Nodes) != 0 || len(merged.Elements.Edges) != 0 {
		t.Errorf("merging nothing should produce an empty graph")
	}
}
