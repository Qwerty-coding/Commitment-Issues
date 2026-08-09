package semantic

import (
	"testing"

	parser "CommitIssues/internal/parser"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

func nodeIDs(scope ConflictScope) map[string]bool {
	ids := make(map[string]bool, len(scope.Nodes))
	for _, n := range scope.Nodes {
		ids[n.ID] = true
	}
	return ids
}

func edgeIDs(scope ConflictScope) map[string]bool {
	ids := make(map[string]bool, len(scope.Edges))
	for _, e := range scope.Edges {
		ids[e.ID] = true
	}
	return ids
}

func fn(name string, line int, content string, calls ...string) parser.CodeElement {
	return parser.CodeElement{Kind: "Function", Name: name, Line: line, Content: content, Calls: calls}
}

func variable(name string, line int, content string) parser.CodeElement {
	return parser.CodeElement{Kind: "Variable", Name: name, Line: line, Content: content}
}

func collision(name string) DiffItem {
	return DiffItem{Type: "COLLISION", Kind: "Function", Name: name}
}

// ─── SideStatus ───────────────────────────────────────────────────────────────

func TestSideStatus_Added(t *testing.T) {
	base := parser.CodeElement{}
	side := fn("login", 1, "function login(){}")
	if got := SideStatus(base, side, false, true); got != "ADDED" {
		t.Errorf("expected ADDED, got %q", got)
	}
}

func TestSideStatus_Deleted(t *testing.T) {
	base := fn("login", 1, "function login(){}")
	side := parser.CodeElement{}
	if got := SideStatus(base, side, true, false); got != "DELETED" {
		t.Errorf("expected DELETED, got %q", got)
	}
}

func TestSideStatus_Updated(t *testing.T) {
	base := fn("login", 1, "function login(){ return 1 }")
	side := fn("login", 1, "function login(){ return 2 }")
	if got := SideStatus(base, side, true, true); got != "UPDATED" {
		t.Errorf("expected UPDATED, got %q", got)
	}
}

func TestSideStatus_Unchanged(t *testing.T) {
	base := fn("login", 1, "function login(){}")
	side := fn("login", 1, "function login(){}")
	if got := SideStatus(base, side, true, true); got != "" {
		t.Errorf("expected empty (unchanged), got %q", got)
	}
}

func TestSideStatus_NotInBaseNotInSide(t *testing.T) {
	// Neither side has it — should produce no status.
	if got := SideStatus(parser.CodeElement{}, parser.CodeElement{}, false, false); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ─── BuildDiffItem ────────────────────────────────────────────────────────────

func TestBuildDiffItem_Deleted(t *testing.T) {
	base := fn("logout", 5, "function logout(){}")
	item := BuildDiffItem("DELETED", base, parser.CodeElement{}, true)
	if item.Type != "DELETED" {
		t.Errorf("expected type DELETED, got %q", item.Type)
	}
	if item.Name != "logout" {
		t.Errorf("expected name logout, got %q", item.Name)
	}
	if item.BaseContent != base.Content {
		t.Errorf("expected BaseContent to be set for DELETED")
	}
	if item.OurContent != "" || item.TheirContent != "" {
		t.Errorf("DELETED item should not have Our/TheirContent")
	}
}

func TestBuildDiffItem_AddedOurs(t *testing.T) {
	side := fn("register", 10, "function register(){}")
	item := BuildDiffItem("ADDED", parser.CodeElement{}, side, true)
	if item.OurContent != side.Content {
		t.Errorf("expected OurContent to be set for ADDED ours")
	}
	if item.TheirContent != "" {
		t.Errorf("TheirContent should be empty for ours=true")
	}
}

func TestBuildDiffItem_AddedTheirs(t *testing.T) {
	side := fn("register", 10, "function register(){}")
	item := BuildDiffItem("ADDED", parser.CodeElement{}, side, false)
	if item.TheirContent != side.Content {
		t.Errorf("expected TheirContent to be set for ADDED theirs")
	}
	if item.OurContent != "" {
		t.Errorf("OurContent should be empty for ours=false")
	}
}

func TestBuildDiffItem_Updated(t *testing.T) {
	base := fn("validate", 3, "old")
	side := fn("validate", 3, "new")
	item := BuildDiffItem("UPDATED", base, side, true)
	if item.BaseContent != "old" {
		t.Errorf("expected BaseContent for UPDATED, got %q", item.BaseContent)
	}
	if item.OurContent != "new" {
		t.Errorf("expected OurContent for UPDATED ours, got %q", item.OurContent)
	}
}

// ─── GenerateSmartDiff ────────────────────────────────────────────────────────

func TestGenerateSmartDiff_Collision(t *testing.T) {
	base := parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "base")}}
	ours := parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "ours")}}
	theirs := parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "theirs")}}

	result := GenerateSmartDiff(base, ours, theirs)

	if len(result.Collisions) != 1 {
		t.Fatalf("expected 1 collision, got %d", len(result.Collisions))
	}
	if result.Collisions[0].Name != "auth" {
		t.Errorf("expected collision on 'auth', got %q", result.Collisions[0].Name)
	}
	if result.Collisions[0].BaseContent != "base" {
		t.Errorf("expected BaseContent='base'")
	}
	if result.Collisions[0].OurContent != "ours" {
		t.Errorf("expected OurContent='ours'")
	}
	if result.Collisions[0].TheirContent != "theirs" {
		t.Errorf("expected TheirContent='theirs'")
	}
	if len(result.OurChanges) != 0 || len(result.TheirChanges) != 0 {
		t.Errorf("collision should not also appear in OurChanges/TheirChanges")
	}
}

func TestGenerateSmartDiff_NoCollision_SameContent(t *testing.T) {
	// Both sides made the same change — not a collision.
	base := parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "old")}}
	ours := parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "new")}}
	theirs := parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "new")}}

	result := GenerateSmartDiff(base, ours, theirs)

	if len(result.Collisions) != 0 {
		t.Errorf("identical changes on both sides should not produce a collision")
	}
}

func TestGenerateSmartDiff_OurAdded(t *testing.T) {
	base := parser.ASTContext{}
	ours := parser.ASTContext{Functions: []parser.CodeElement{fn("newFunc", 5, "code")}}
	theirs := parser.ASTContext{}

	result := GenerateSmartDiff(base, ours, theirs)

	if len(result.OurChanges) != 1 {
		t.Fatalf("expected 1 OurChange, got %d", len(result.OurChanges))
	}
	if result.OurChanges[0].Type != "ADDED" {
		t.Errorf("expected ADDED, got %q", result.OurChanges[0].Type)
	}
	if len(result.Collisions) != 0 || len(result.TheirChanges) != 0 {
		t.Errorf("unexpected collisions or their changes")
	}
}

func TestGenerateSmartDiff_TheirDeleted(t *testing.T) {
	base := parser.ASTContext{Functions: []parser.CodeElement{fn("oldFunc", 2, "code")}}
	ours := parser.ASTContext{Functions: []parser.CodeElement{fn("oldFunc", 2, "code")}}
	theirs := parser.ASTContext{}

	result := GenerateSmartDiff(base, ours, theirs)

	if len(result.TheirChanges) != 1 {
		t.Fatalf("expected 1 TheirChange, got %d", len(result.TheirChanges))
	}
	if result.TheirChanges[0].Type != "DELETED" {
		t.Errorf("expected DELETED, got %q", result.TheirChanges[0].Type)
	}
}

func TestGenerateSmartDiff_EmptyEverything(t *testing.T) {
	result := GenerateSmartDiff(parser.ASTContext{}, parser.ASTContext{}, parser.ASTContext{})
	if len(result.Collisions) != 0 || len(result.OurChanges) != 0 || len(result.TheirChanges) != 0 {
		t.Errorf("all-empty diff should produce no results")
	}
}

func TestGenerateSmartDiff_CollisionsSortedByName(t *testing.T) {
	base := parser.ASTContext{Functions: []parser.CodeElement{
		fn("zebra", 1, "b"), fn("alpha", 2, "b"),
	}}
	ours := parser.ASTContext{Functions: []parser.CodeElement{
		fn("zebra", 1, "o"), fn("alpha", 2, "o"),
	}}
	theirs := parser.ASTContext{Functions: []parser.CodeElement{
		fn("zebra", 1, "t"), fn("alpha", 2, "t"),
	}}

	result := GenerateSmartDiff(base, ours, theirs)

	if len(result.Collisions) != 2 {
		t.Fatalf("expected 2 collisions, got %d", len(result.Collisions))
	}
	if result.Collisions[0].Name != "alpha" || result.Collisions[1].Name != "zebra" {
		t.Errorf("collisions not sorted by name: got %q, %q",
			result.Collisions[0].Name, result.Collisions[1].Name)
	}
}

func TestGenerateSmartDiff_OurChangesSortedByStatusThenLine(t *testing.T) {
	base := parser.ASTContext{Functions: []parser.CodeElement{fn("existing", 10, "old")}}
	ours := parser.ASTContext{Functions: []parser.CodeElement{
		fn("newFunc", 1, "new"),    // ADDED at line 1
		fn("existing", 10, "upd"), // UPDATED at line 10
	}}
	theirs := parser.ASTContext{Functions: []parser.CodeElement{fn("existing", 10, "old")}}

	result := GenerateSmartDiff(base, ours, theirs)

	if len(result.OurChanges) != 2 {
		t.Fatalf("expected 2 OurChanges, got %d", len(result.OurChanges))
	}
	// ADDED (rank 0) should come before UPDATED (rank 1)
	if result.OurChanges[0].Type != "ADDED" {
		t.Errorf("expected ADDED first, got %q", result.OurChanges[0].Type)
	}
	if result.OurChanges[1].Type != "UPDATED" {
		t.Errorf("expected UPDATED second, got %q", result.OurChanges[1].Type)
	}
}

// ─── BuildSemanticGraph ───────────────────────────────────────────────────────

func TestBuildSemanticGraph_Empty(t *testing.T) {
	g := BuildSemanticGraph(parser.ASTContext{})
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Errorf("empty context should produce empty graph")
	}
}

func TestBuildSemanticGraph_FunctionsAndVariables(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{fn("login", 1, "code")},
		Variables: []parser.CodeElement{variable("token", 5, "var token")},
	}
	g := BuildSemanticGraph(ctx)

	if len(g.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(g.Nodes))
	}
}

func TestBuildSemanticGraph_CallsEdge(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("login", 1, "code", "validateUser"),
			fn("validateUser", 5, "code"),
		},
	}
	g := BuildSemanticGraph(ctx)

	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(g.Edges))
	}
	if g.Edges[0].Type != "CALLS" {
		t.Errorf("expected edge type CALLS, got %q", g.Edges[0].Type)
	}
}

func TestBuildSemanticGraph_CallToUnknownFunctionIgnored(t *testing.T) {
	// login calls "externalLib" which isn't in the AST — no edge should appear.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("login", 1, "code", "externalLib"),
		},
	}
	g := BuildSemanticGraph(ctx)

	if len(g.Edges) != 0 {
		t.Errorf("call to unknown function should not create an edge")
	}
}

func TestBuildSemanticGraph_NoDuplicateEdges(t *testing.T) {
	// login lists validateUser twice in Calls — should still produce 1 edge.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("login", 1, "code", "validateUser", "validateUser"),
			fn("validateUser", 5, "code"),
		},
	}
	g := BuildSemanticGraph(ctx)

	if len(g.Edges) != 1 {
		t.Errorf("duplicate call entries should produce exactly 1 edge, got %d", len(g.Edges))
	}
}

func TestBuildSemanticGraph_NodeIDs(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{fn("My Func", 1, "code")},
	}
	g := BuildSemanticGraph(ctx)

	// Spaces in names should be sanitized in the ID.
	id := g.Nodes[0].ID
	for _, ch := range []string{" "} {
		for _, r := range id {
			if string(r) == ch {
				t.Errorf("node ID %q contains unsanitized character %q", id, ch)
			}
		}
	}
}

// ─── MergeSemanticGraphs ──────────────────────────────────────────────────────

func TestMergeSemanticGraphs_Empty(t *testing.T) {
	merged := MergeSemanticGraphs()
	if len(merged.Nodes) != 0 || len(merged.Edges) != 0 {
		t.Errorf("merging nothing should return empty graph")
	}
}

func TestMergeSemanticGraphs_DeduplicatesNodes(t *testing.T) {
	ctx := parser.ASTContext{Functions: []parser.CodeElement{fn("shared", 1, "code")}}
	g1 := BuildSemanticGraph(ctx)
	g2 := BuildSemanticGraph(ctx)

	merged := MergeSemanticGraphs(g1, g2)
	if len(merged.Nodes) != 1 {
		t.Errorf("same node from two graphs should be deduplicated; got %d nodes", len(merged.Nodes))
	}
}

func TestMergeSemanticGraphs_DeduplicatesEdges(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("a", 1, "code", "b"),
			fn("b", 2, "code"),
		},
	}
	g1 := BuildSemanticGraph(ctx)
	g2 := BuildSemanticGraph(ctx)

	merged := MergeSemanticGraphs(g1, g2)
	if len(merged.Edges) != 1 {
		t.Errorf("same edge from two graphs should be deduplicated; got %d edges", len(merged.Edges))
	}
}

func TestMergeSemanticGraphs_UniquesFromBothSides(t *testing.T) {
	g1 := BuildSemanticGraph(parser.ASTContext{
		Functions: []parser.CodeElement{fn("funcA", 1, "code")},
	})
	g2 := BuildSemanticGraph(parser.ASTContext{
		Functions: []parser.CodeElement{fn("funcB", 2, "code")},
	})

	merged := MergeSemanticGraphs(g1, g2)
	if len(merged.Nodes) != 2 {
		t.Errorf("expected 2 unique nodes, got %d", len(merged.Nodes))
	}
}

// ─── ComputeConflictScope ─────────────────────────────────────────────────────

func TestComputeConflictScope_EmptyGraph(t *testing.T) {
	scope := ComputeConflictScope(SemanticGraph{}, []DiffItem{collision("auth")})
	if len(scope.Nodes) != 0 || len(scope.Edges) != 0 {
		t.Errorf("empty graph should produce empty scope")
	}
}

func TestComputeConflictScope_NoCollisions(t *testing.T) {
	ctx := parser.ASTContext{Functions: []parser.CodeElement{fn("login", 1, "code")}}
	g := BuildSemanticGraph(ctx)

	scope := ComputeConflictScope(g, []DiffItem{})
	if len(scope.Nodes) != 0 {
		t.Errorf("no collisions should produce empty scope")
	}
}

func TestComputeConflictScope_CollisionRootIncluded(t *testing.T) {
	ctx := parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "code")}}
	g := BuildSemanticGraph(ctx)

	scope := ComputeConflictScope(g, []DiffItem{collision("auth")})

	ids := nodeIDs(scope)
	authID := semanticNodeID("Function", "auth")
	if !ids[authID] {
		t.Errorf("collision root 'auth' should be in scope")
	}
}

func TestComputeConflictScope_FollowsOutgoingEdge(t *testing.T) {
	// auth → validateUser: scope from auth should include validateUser.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("auth", 1, "code", "validateUser"),
			fn("validateUser", 5, "code"),
			fn("unrelated", 10, "code"),
		},
	}
	g := BuildSemanticGraph(ctx)
	scope := ComputeConflictScope(g, []DiffItem{collision("auth")})

	ids := nodeIDs(scope)
	if !ids[semanticNodeID("Function", "auth")] {
		t.Errorf("auth should be in scope")
	}
	if !ids[semanticNodeID("Function", "validateUser")] {
		t.Errorf("validateUser (callee of auth) should be in scope")
	}
	if ids[semanticNodeID("Function", "unrelated")] {
		t.Errorf("unrelated should NOT be in scope")
	}
}

func TestComputeConflictScope_FollowsIncomingEdge(t *testing.T) {
	// login → auth: collision on auth should pull in login (its caller).
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("login", 1, "code", "auth"),
			fn("auth", 5, "code"),
			fn("unrelated", 10, "code"),
		},
	}
	g := BuildSemanticGraph(ctx)
	scope := ComputeConflictScope(g, []DiffItem{collision("auth")})

	ids := nodeIDs(scope)
	if !ids[semanticNodeID("Function", "login")] {
		t.Errorf("login (caller of auth) should be in scope via incoming edge")
	}
	if ids[semanticNodeID("Function", "unrelated")] {
		t.Errorf("unrelated should NOT be in scope")
	}
}

func TestComputeConflictScope_TransitiveDependencies(t *testing.T) {
	// auth → validateUser → hashPassword: all three should be in scope.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("auth", 1, "code", "validateUser"),
			fn("validateUser", 5, "code", "hashPassword"),
			fn("hashPassword", 10, "code"),
			fn("unrelated", 20, "code"),
		},
	}
	g := BuildSemanticGraph(ctx)
	scope := ComputeConflictScope(g, []DiffItem{collision("auth")})

	ids := nodeIDs(scope)
	for _, name := range []string{"auth", "validateUser", "hashPassword"} {
		if !ids[semanticNodeID("Function", name)] {
			t.Errorf("%q should be in transitive scope", name)
		}
	}
	if ids[semanticNodeID("Function", "unrelated")] {
		t.Errorf("unrelated should NOT be in transitive scope")
	}
}

func TestComputeConflictScope_EdgesIncluded(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("auth", 1, "code", "validateUser"),
			fn("validateUser", 5, "code"),
		},
	}
	g := BuildSemanticGraph(ctx)
	scope := ComputeConflictScope(g, []DiffItem{collision("auth")})

	if len(scope.Edges) != 1 {
		t.Errorf("expected 1 edge in scope, got %d", len(scope.Edges))
	}
	eids := edgeIDs(scope)
	expectedEdge := semanticNodeID("Function", "auth") + "__CALLS__" + semanticNodeID("Function", "validateUser")
	if !eids[expectedEdge] {
		t.Errorf("CALLS edge auth→validateUser should be in scope")
	}
}

func TestComputeConflictScope_MultipleCollisionRoots(t *testing.T) {
	// Two independent collision roots — scope should include both.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("authA", 1, "code", "helper"),
			fn("authB", 5, "code"),
			fn("helper", 10, "code"),
		},
	}
	g := BuildSemanticGraph(ctx)
	scope := ComputeConflictScope(g, []DiffItem{collision("authA"), collision("authB")})

	ids := nodeIDs(scope)
	for _, name := range []string{"authA", "authB", "helper"} {
		if !ids[semanticNodeID("Function", name)] {
			t.Errorf("%q should be in scope from multiple roots", name)
		}
	}
}

func TestComputeConflictScope_VariableCollisionIgnored(t *testing.T) {
	// ComputeConflictScope only seeds from Function collisions.
	// A Variable collision should not seed the BFS.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{fn("login", 1, "code")},
		Variables: []parser.CodeElement{variable("token", 5, "var token")},
	}
	g := BuildSemanticGraph(ctx)

	varCollision := DiffItem{Type: "COLLISION", Kind: "Variable", Name: "token"}
	scope := ComputeConflictScope(g, []DiffItem{varCollision})

	if len(scope.Nodes) != 0 {
		t.Errorf("variable collision should not seed BFS; got %d nodes in scope", len(scope.Nodes))
	}
}

func TestComputeConflictScope_CyclicCallGraph(t *testing.T) {
	// a → b → a: BFS must terminate, not loop forever.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			fn("a", 1, "code", "b"),
			fn("b", 5, "code", "a"),
		},
	}
	g := BuildSemanticGraph(ctx)
	scope := ComputeConflictScope(g, []DiffItem{collision("a")})

	ids := nodeIDs(scope)
	if !ids[semanticNodeID("Function", "a")] || !ids[semanticNodeID("Function", "b")] {
		t.Errorf("both nodes of a cycle should be in scope")
	}
	if len(scope.Nodes) != 2 {
		t.Errorf("cycle should not produce duplicate nodes; got %d", len(scope.Nodes))
	}
}

func TestComputeConflictScope_CollisionOnUnknownFunction(t *testing.T) {
	// Collision names a function that doesn't exist in the graph — should not panic.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{fn("login", 1, "code")},
	}
	g := BuildSemanticGraph(ctx)
	scope := ComputeConflictScope(g, []DiffItem{collision("ghost")})

	if len(scope.Nodes) != 0 {
		t.Errorf("unknown collision root should produce empty scope; got %d nodes", len(scope.Nodes))
	}
}

func TestComputeConflictScope_CaseInsensitiveKindMatch(t *testing.T) {
	// "function" (lowercase) in DiffItem.Kind should still match.
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{fn("auth", 1, "code")},
	}
	g := BuildSemanticGraph(ctx)

	lowerKindCollision := DiffItem{Type: "COLLISION", Kind: "function", Name: "auth"}
	scope := ComputeConflictScope(g, []DiffItem{lowerKindCollision})

	ids := nodeIDs(scope)
	if !ids[semanticNodeID("Function", "auth")] {
		t.Errorf("kind matching should be case-insensitive")
	}
}

// ─── BuildSignatureMap ────────────────────────────────────────────────────────

func TestBuildSignatureMap_FunctionsAndVariables(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{fn("login", 1, "code")},
		Variables: []parser.CodeElement{variable("token", 2, "var")},
	}
	m := BuildSignatureMap(ctx)

	if _, ok := m["Function:login"]; !ok {
		t.Errorf("expected Function:login in signature map")
	}
	if _, ok := m["Variable:token"]; !ok {
		t.Errorf("expected Variable:token in signature map")
	}
}

func TestBuildSignatureMap_Empty(t *testing.T) {
	m := BuildSignatureMap(parser.ASTContext{})
	if len(m) != 0 {
		t.Errorf("expected empty map for empty context")
	}
}