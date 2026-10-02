package semantic

import (
	"testing"

	parser "CommitIssues/internal/parser"
)

func scopedFn(scope, name, signature, content string, calls ...string) parser.CodeElement {
	return parser.CodeElement{
		Kind:      "Function",
		Name:      name,
		Scope:     scope,
		Signature: signature,
		Content:   content,
		Calls:     calls,
	}
}

// ─── SymbolIdentity ──────────────────────────────────────────────────────────

func TestSymbolIdentity_FallbackToKindName(t *testing.T) {
	el := fn("login", 1, "code")
	if got := SymbolIdentity(el); got != "Function:login" {
		t.Errorf("identity = %q, want Function:login", got)
	}
}

func TestSymbolIdentity_IncludesFileScopeKindNameSignature(t *testing.T) {
	el := parser.CodeElement{
		File: "a.go", Scope: "Engine", Kind: "Function", Name: "charge", Signature: "(t Token)",
	}
	want := "a.go|Engine|Function|charge|(t Token)"
	if got := SymbolIdentity(el); got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

func TestDiffKey_UsesIdentityWhenPresent(t *testing.T) {
	withIdentity := DiffItem{Kind: "Function", Name: "run", Identity: "a.go|A|Function|run|()"}
	if got := DiffKey(withIdentity); got != "a.go|A|Function|run|()" {
		t.Errorf("DiffKey = %q, want the identity", got)
	}
	withoutIdentity := DiffItem{Kind: "Function", Name: "run"}
	if got := DiffKey(withoutIdentity); got != "function:run" {
		t.Errorf("DiffKey fallback = %q, want function:run", got)
	}
}

// ─── Distinct collisions per scope / signature ───────────────────────────────

func TestGenerateSmartDiff_DistinctScopesStayDistinct(t *testing.T) {
	ours := parser.ASTContext{Functions: []parser.CodeElement{
		scopedFn("A", "run", "", "ours-in-A"),
		scopedFn("B", "run", "", "ours-in-B"),
	}}
	theirs := parser.ASTContext{Functions: []parser.CodeElement{
		scopedFn("A", "run", "", "theirs-in-A"),
		scopedFn("B", "run", "", "theirs-in-B"),
	}}

	result := GenerateSmartDiff(parser.ASTContext{}, ours, theirs)

	if len(result.Collisions) != 2 {
		t.Fatalf("expected 2 distinct collisions for two scopes, got %d: %#v", len(result.Collisions), result.Collisions)
	}
	identities := map[string]bool{}
	for _, c := range result.Collisions {
		identities[c.Identity] = true
	}
	if len(identities) != 2 {
		t.Errorf("collision identities must be distinct, got %v", identities)
	}
}

func TestGenerateSmartDiff_OverloadsDistinctBySignature(t *testing.T) {
	ours := parser.ASTContext{Functions: []parser.CodeElement{
		scopedFn("", "parse", "(int)", "ours-int"),
		scopedFn("", "parse", "(string)", "ours-string"),
	}}
	theirs := parser.ASTContext{Functions: []parser.CodeElement{
		scopedFn("", "parse", "(int)", "theirs-int"),
		scopedFn("", "parse", "(string)", "theirs-string"),
	}}

	result := GenerateSmartDiff(parser.ASTContext{}, ours, theirs)
	if len(result.Collisions) != 2 {
		t.Fatalf("expected 2 collisions for overloads, got %d", len(result.Collisions))
	}
}

func TestBuildSemanticGraph_DistinctScopesGetDistinctNodeIDs(t *testing.T) {
	ctx := parser.ASTContext{Functions: []parser.CodeElement{
		scopedFn("A", "run", "", "a"),
		scopedFn("B", "run", "", "b"),
	}}
	g := BuildSemanticGraph(ctx)

	if len(g.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(g.Nodes))
	}
	if g.Nodes[0].ID == g.Nodes[1].ID {
		t.Errorf("nodes in different scopes must have distinct IDs: %q", g.Nodes[0].ID)
	}
	if g.Nodes[0].Identity == g.Nodes[1].Identity {
		t.Errorf("nodes in different scopes must have distinct identities")
	}
}

func TestBuildSemanticGraph_LegacyIDsPreserved(t *testing.T) {
	// Elements without scope/signature/file must still produce the legacy IDs
	// so existing behavior is unchanged.
	g := BuildSemanticGraph(parser.ASTContext{Functions: []parser.CodeElement{fn("auth", 1, "code")}})
	if got := g.Nodes[0].ID; got != semanticNodeID("Function", "auth") {
		t.Errorf("legacy node ID = %q, want %q", got, semanticNodeID("Function", "auth"))
	}
}

func TestComputeConflictScope_MatchesPreciseIdentity(t *testing.T) {
	ctx := parser.ASTContext{Functions: []parser.CodeElement{
		scopedFn("A", "run", "", "a"),
		scopedFn("B", "run", "", "b", "helper"),
		scopedFn("B", "helper", "", "h"),
	}}
	g := BuildSemanticGraph(ctx)

	collision := DiffItem{
		Type: "COLLISION", Kind: "Function", Name: "run",
		Identity: SymbolIdentity(scopedFn("B", "run", "", "b", "helper")),
	}
	scope := ComputeConflictScope(g, []DiffItem{collision})

	ids := nodeIDs(scope)
	runB := semanticNodeIDFor(scopedFn("B", "run", "", "b"))
	runA := semanticNodeIDFor(scopedFn("A", "run", "", "a"))
	helperB := semanticNodeIDFor(scopedFn("B", "helper", "", "h"))

	if !ids[runB] {
		t.Errorf("scope should include B.run")
	}
	if !ids[helperB] {
		t.Errorf("scope should follow B.run -> helper edge")
	}
	if ids[runA] {
		t.Errorf("scope must not include A.run for a B.run collision")
	}
}
