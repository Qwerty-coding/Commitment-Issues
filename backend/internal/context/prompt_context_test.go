package promptcontext

import (
	"strings"
	"testing"

	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

func TestRenderPromptContext_UsesLanguageFence(t *testing.T) {
	functions := []parser.CodeElement{{
		Name: "calculate", Kind: "Function", Line: 3, Content: "def calculate():\n    return 1",
	}}

	out := renderPromptContext("repo", "", []string{"app.py"}, functions, nil, nil, "python")
	if !strings.Contains(out, "```python\n") {
		t.Fatalf("expected python fence, got:\n%s", out)
	}
	if strings.Contains(out, "```javascript") {
		t.Fatalf("must never fence non-JS content as javascript:\n%s", out)
	}
}

func TestRenderPromptContext_UnknownLanguageBareFence(t *testing.T) {
	functions := []parser.CodeElement{{
		Name: "x", Kind: "Function", Line: 1, Content: "body",
	}}

	out := renderPromptContext("repo", "", []string{"a.unknownext"}, functions, nil, nil, "")
	if !strings.Contains(out, "```\n") {
		t.Fatalf("expected bare fence for unknown language, got:\n%s", out)
	}
	if strings.Contains(out, "```javascript") {
		t.Fatalf("bare fence must not be labeled javascript:\n%s", out)
	}
}

func TestRenderPromptContext_RendersImports(t *testing.T) {
	withImports := renderPromptContext("repo", "", []string{"a.go"}, nil, nil, []string{"import \"fmt\""}, "go")
	if !strings.Contains(withImports, "Imports:\n- import \"fmt\"\n") {
		t.Fatalf("expected rendered import, got:\n%s", withImports)
	}

	without := renderPromptContext("repo", "", []string{"a.go"}, nil, nil, nil, "go")
	if !strings.Contains(without, "Imports:\n- (none)\n") {
		t.Fatalf("expected empty imports marker, got:\n%s", without)
	}
}

func TestRenderPromptContext_RendersReadmeSection(t *testing.T) {
	out := renderPromptContext("repo", "# My Project\nA great tool.\n", []string{"a.go"}, nil, nil, nil, "go")
	if !strings.Contains(out, "Repository README (excerpt):") {
		t.Fatalf("expected README section header, got:\n%s", out)
	}
	if !strings.Contains(out, "A great tool.") {
		t.Fatalf("expected README content, got:\n%s", out)
	}
	// README section must appear before "Files in scope".
	readmeIdx := strings.Index(out, "Repository README (excerpt):")
	filesIdx := strings.Index(out, "Files in scope:")
	if readmeIdx >= filesIdx {
		t.Fatalf("README section (%d) must precede Files in scope (%d)", readmeIdx, filesIdx)
	}
}

func TestRenderPromptContext_NoReadmeSection_WhenEmpty(t *testing.T) {
	out := renderPromptContext("repo", "", []string{"a.go"}, nil, nil, nil, "go")
	if strings.Contains(out, "Repository README (excerpt):") {
		t.Fatalf("should not render README section when readme is empty, got:\n%s", out)
	}
}

func TestMergeImports_DedupesSortsAndSkipsEmpty(t *testing.T) {
	ours := parser.ASTContext{Imports: []parser.CodeElement{
		{Content: "import zlib"},
		{Content: "import os"},
		{Content: "   "},
	}}
	theirs := parser.ASTContext{Imports: []parser.CodeElement{
		{Content: "import os"},
		{Content: "import sys"},
	}}

	merged := mergeImports(ours, theirs)
	want := []string{"import os", "import sys", "import zlib"}
	if len(merged) != len(want) {
		t.Fatalf("expected %d imports, got %d: %v", len(want), len(merged), merged)
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Fatalf("merged[%d] = %q, want %q (full: %v)", i, merged[i], want[i], merged)
		}
	}
}

func TestBuildPromptContext_PopulatesImportsAndLanguageFence(t *testing.T) {
	fn := parser.CodeElement{Name: "run", Kind: "Function", Line: 5, Content: "func run() {}"}
	ours := parser.ASTContext{
		Functions: []parser.CodeElement{fn},
		Imports:   []parser.CodeElement{{Content: "import \"os\""}},
	}
	scope := semantic.SemanticGraph{Nodes: []semantic.SemanticNode{{
		Kind: "Function", Identity: semantic.SymbolIdentity(fn),
	}}}

	ir := BuildPromptContext("repo", "", []string{"main.go"}, scope, ours, parser.ASTContext{})

	if len(ir.Imports) != 1 || ir.Imports[0] != "import \"os\"" {
		t.Fatalf("expected imports populated, got %v", ir.Imports)
	}
	if !strings.Contains(ir.Context, "```go\n") {
		t.Fatalf("expected go fence derived from file extension, got:\n%s", ir.Context)
	}
	if !strings.Contains(ir.Context, "- import \"os\"") {
		t.Fatalf("expected import rendered in context, got:\n%s", ir.Context)
	}
}

func TestBuildPromptContext_PopulatesRepositoryReadme(t *testing.T) {
	readme := "# Acme Corp\nA conflict resolver.\n"
	ir := BuildPromptContext("repo", readme, []string{"main.go"}, semantic.SemanticGraph{}, parser.ASTContext{}, parser.ASTContext{})

	if ir.RepositoryReadme != readme {
		t.Fatalf("RepositoryReadme=%q, want %q", ir.RepositoryReadme, readme)
	}
	if !strings.Contains(ir.Context, "Repository README (excerpt):") {
		t.Fatalf("Context should contain README section, got:\n%s", ir.Context)
	}
}

func TestBuildPromptContext_EmptyReadme_NoSection(t *testing.T) {
	ir := BuildPromptContext("repo", "", []string{"main.go"}, semantic.SemanticGraph{}, parser.ASTContext{}, parser.ASTContext{})

	if ir.RepositoryReadme != "" {
		t.Fatalf("RepositoryReadme should be empty, got %q", ir.RepositoryReadme)
	}
	if strings.Contains(ir.Context, "Repository README (excerpt):") {
		t.Fatalf("Context should not contain README section when readme is empty:\n%s", ir.Context)
	}
}
