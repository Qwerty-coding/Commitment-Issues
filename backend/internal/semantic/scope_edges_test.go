package semantic

import (
	"testing"

	parser "CommitIssues/internal/parser"
)

// TestBuildSemanticGraph_ScopeAwareEdges verifies that a bare method name
// shared across classes does NOT produce a false cross-class CALLS edge, and
// that a same-scope call IS resolved.
func TestBuildSemanticGraph_ScopeAwareEdges(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			// Class A: a.run() calls a.toString() (same scope) and helper().
			{File: "a.ts", Scope: "A", Kind: "Method", Name: "run", Signature: "()", Calls: []string{"toString", "helper"}},
			{File: "a.ts", Scope: "A", Kind: "Method", Name: "toString", Signature: "()"},
			// Class B: an unrelated toString() in another class.
			{File: "a.ts", Scope: "B", Kind: "Method", Name: "toString", Signature: "()"},
			// A unique global function.
			{File: "a.ts", Scope: "", Kind: "Function", Name: "helper", Signature: "()"},
		},
	}

	graph := BuildSemanticGraph(ctx)

	// Expect exactly 2 CALLS edges: A.run -> A.toString and A.run -> helper.
	// There must be NO edge from A.run to B.toString.
	edges := graph.Edges
	if len(edges) != 2 {
		t.Fatalf("expected 2 CALLS edges, got %d: %+v", len(edges), edges)
	}

	var runID, aToStringID, bToStringID string
	for _, n := range graph.Nodes {
		switch {
		case n.Name == "run":
			runID = n.ID
		case n.Name == "toString" && n.Identity != "" && contains(n.Identity, "|A|"):
			aToStringID = n.ID
		case n.Name == "toString" && contains(n.Identity, "|B|"):
			bToStringID = n.ID
		}
	}

	targets := map[string]bool{}
	for _, e := range edges {
		if e.Source != runID {
			t.Errorf("unexpected edge source %s", e.Source)
		}
		targets[e.Target] = true
	}
	if !targets[aToStringID] {
		t.Errorf("expected edge A.run -> A.toString (%s)", aToStringID)
	}
	if targets[bToStringID] {
		t.Errorf("false cross-class edge A.run -> B.toString (%s)", bToStringID)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
