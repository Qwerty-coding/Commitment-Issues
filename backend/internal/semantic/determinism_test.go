package semantic

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	parser "CommitIssues/internal/parser"
)

// ─── Repeated runs ───────────────────────────────────────────────────────────

func manySymbolContext(content string) parser.ASTContext {
	fns := make([]parser.CodeElement, 0, 25)
	for i := 0; i < 25; i++ {
		fns = append(fns, parser.CodeElement{
			Kind:    "Function",
			Name:    fmt.Sprintf("fn%02d", i),
			Line:    i,
			Content: content,
		})
	}
	return parser.ASTContext{Functions: fns}
}

func TestGenerateSmartDiff_RepeatedRunsAreIdentical(t *testing.T) {
	base := manySymbolContext("base")
	ours := manySymbolContext("ours")
	theirs := manySymbolContext("theirs")

	first := GenerateSmartDiff(base, ours, theirs)
	for i := 0; i < 25; i++ {
		if got := GenerateSmartDiff(base, ours, theirs); !reflect.DeepEqual(first, got) {
			t.Fatalf("run %d produced different output; map iteration order leaked into results", i)
		}
	}
}

func TestBuildSemanticGraph_RepeatedRunsAreIdentical(t *testing.T) {
	ctx := parser.ASTContext{
		Functions: []parser.CodeElement{
			{Kind: "Function", Name: "b", Line: 2, Content: "b", Calls: []string{"a"}},
			{Kind: "Function", Name: "a", Line: 1, Content: "a"},
			{Kind: "Class", Name: "Engine", Line: 3, Content: "c"},
		},
		Variables: []parser.CodeElement{{Kind: "Variable", Name: "token", Line: 4, Content: "v"}},
	}

	first := BuildSemanticGraph(ctx)
	for i := 0; i < 25; i++ {
		if got := BuildSemanticGraph(ctx); !reflect.DeepEqual(first, got) {
			t.Fatalf("semantic graph output is not deterministic")
		}
	}
}

// ─── Stable sort tie-breakers ────────────────────────────────────────────────

func TestSortDiffItems_CanonicalTieBreakers(t *testing.T) {
	items := []DiffItem{
		{Identity: "b", File: "a", Kind: "Function", Name: "n", Line: 1, Type: "ADDED"},
		{Identity: "a", File: "z", Kind: "Function", Name: "n", Line: 9, Type: "UPDATED"},
		{Identity: "a", File: "a", Kind: "Function", Name: "n", Line: 9, Type: "UPDATED"},
		{Identity: "a", File: "a", Kind: "Class", Name: "n", Line: 9, Type: "UPDATED"},
		{Identity: "a", File: "a", Kind: "Class", Name: "m", Line: 9, Type: "UPDATED"},
		{Identity: "a", File: "a", Kind: "Class", Name: "m", Line: 3, Type: "UPDATED"},
		{Identity: "a", File: "a", Kind: "Class", Name: "m", Line: 3, Type: "ADDED"},
	}

	SortDiffItems(items)

	want := []struct {
		identity string
		file     string
		kind     string
		name     string
		line     int
		typ      string
	}{
		{"a", "a", "Class", "m", 3, "ADDED"},
		{"a", "a", "Class", "m", 3, "UPDATED"},
		{"a", "a", "Class", "m", 9, "UPDATED"},
		{"a", "a", "Class", "n", 9, "UPDATED"},
		{"a", "a", "Function", "n", 9, "UPDATED"},
		{"a", "z", "Function", "n", 9, "UPDATED"},
		{"b", "a", "Function", "n", 1, "ADDED"},
	}

	for i, w := range want {
		got := items[i]
		if got.Identity != w.identity || got.File != w.file || got.Kind != w.kind ||
			got.Name != w.name || got.Line != w.line || got.Type != w.typ {
			t.Fatalf("index %d = %+v, want %+v", i, got, w)
		}
		if i > 0 && CompareDiffItems(items[i-1], items[i]) > 0 {
			t.Fatalf("order not sorted at index %d", i)
		}
	}
}

func TestCompareDiffItems_IsTotalOrder(t *testing.T) {
	items := []DiffItem{
		{Identity: "x", File: "b", Kind: "Function", Name: "a", Line: 2, Type: "ADDED"},
		{Identity: "x", File: "b", Kind: "Function", Name: "a", Line: 2, Type: "UPDATED"},
		{Identity: "x", File: "b", Kind: "Function", Name: "a", Line: 1, Type: "ADDED"},
		{Identity: "x", File: "a", Kind: "Function", Name: "a", Line: 1, Type: "ADDED"},
	}
	sorted := append([]DiffItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return CompareDiffItems(sorted[i], sorted[j]) < 0 })

	for i := 1; i < len(sorted); i++ {
		if CompareDiffItems(sorted[i-1], sorted[i]) > 0 {
			t.Fatalf("sort produced a non-total order at %d", i)
		}
	}
}
