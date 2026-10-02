package semantic

import (
	"testing"

	parser "CommitIssues/internal/parser"
)

// ─── Non-function collision kinds ────────────────────────────────────────────

func TestComputeConflictScope_ClassCollision(t *testing.T) {
	engine := parser.CodeElement{Kind: "Class", Name: "Engine", Line: 1, Content: "class Engine {}"}
	g := BuildSemanticGraph(parser.ASTContext{Functions: []parser.CodeElement{engine}})

	scope := ComputeConflictScope(g, []DiffItem{{Type: "COLLISION", Kind: "Class", Name: "Engine"}})
	if !nodeIDs(scope)[semanticNodeIDFor(engine)] {
		t.Errorf("class collision should be in scope; got %d nodes", len(scope.Nodes))
	}
}

func TestComputeConflictScope_MethodCollision(t *testing.T) {
	charge := parser.CodeElement{Kind: "Function", Name: "charge", Scope: "Engine", Line: 5, Content: "fn"}
	g := BuildSemanticGraph(parser.ASTContext{Functions: []parser.CodeElement{charge}})

	collision := DiffItem{Type: "COLLISION", Kind: "Function", Name: "charge", Identity: SymbolIdentity(charge)}
	scope := ComputeConflictScope(g, []DiffItem{collision})
	if !nodeIDs(scope)[semanticNodeIDFor(charge)] {
		t.Errorf("method collision should be in scope; got %d nodes", len(scope.Nodes))
	}
}

func TestComputeConflictScope_MethodCollisionLegacyFallback(t *testing.T) {
	// A collision without an identity must still fall back to kind:name matching.
	charge := parser.CodeElement{Kind: "Function", Name: "charge", Scope: "Engine", Line: 5, Content: "fn"}
	g := BuildSemanticGraph(parser.ASTContext{Functions: []parser.CodeElement{charge}})

	collision := DiffItem{Type: "COLLISION", Kind: "Function", Name: "charge"}
	scope := ComputeConflictScope(g, []DiffItem{collision})
	if !nodeIDs(scope)[semanticNodeIDFor(charge)] {
		t.Errorf("legacy kind:name fallback should still match a scoped method")
	}
}

func TestComputeConflictScope_ConstructorCollision(t *testing.T) {
	ctor := parser.CodeElement{Kind: "Constructor", Name: "NewClient", Line: 3, Content: "ctor"}
	g := BuildSemanticGraph(parser.ASTContext{Functions: []parser.CodeElement{ctor}})

	scope := ComputeConflictScope(g, []DiffItem{{Type: "COLLISION", Kind: "Constructor", Name: "NewClient"}})
	if !nodeIDs(scope)[semanticNodeIDFor(ctor)] {
		t.Errorf("constructor collision should be in scope; got %d nodes", len(scope.Nodes))
	}
}

func TestComputeConflictScope_VariableCollision(t *testing.T) {
	token := parser.CodeElement{Kind: "Variable", Name: "token", Line: 2, Content: "var token"}
	g := BuildSemanticGraph(parser.ASTContext{Variables: []parser.CodeElement{token}})

	scope := ComputeConflictScope(g, []DiffItem{{Type: "COLLISION", Kind: "Variable", Name: "token"}})
	if !nodeIDs(scope)[semanticNodeIDFor(token)] {
		t.Errorf("variable collision should be in scope; got %d nodes", len(scope.Nodes))
	}
}

func TestComputeConflictScope_UnknownFutureKind(t *testing.T) {
	iface := parser.CodeElement{Kind: "Interface", Name: "Reader", Line: 7, Content: "interface"}
	g := BuildSemanticGraph(parser.ASTContext{Functions: []parser.CodeElement{iface}})

	scope := ComputeConflictScope(g, []DiffItem{{Type: "COLLISION", Kind: "Interface", Name: "Reader"}})
	if !nodeIDs(scope)[semanticNodeIDFor(iface)] {
		t.Errorf("unknown future kinds should still seed the scope; got %d nodes", len(scope.Nodes))
	}
}
