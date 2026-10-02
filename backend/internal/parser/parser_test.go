package parser

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	js "github.com/smacker/go-tree-sitter/javascript"
	py "github.com/smacker/go-tree-sitter/python"
)

func parseWith(t *testing.T, lang *sitter.Language, code string) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(lang)
	tree, err := p.ParseCtx(context.Background(), nil, []byte(code))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	return tree
}

func findFunction(elements []CodeElement, name string) (CodeElement, bool) {
	for _, el := range elements {
		if el.Name == name {
			return el, true
		}
	}
	return CodeElement{}, false
}

func TestExtractDataForFile_SetsFileOnEveryElement(t *testing.T) {
	code := "function login() { return 1; }\nvar token = 'x';\n"
	tree := parseWith(t, js.GetLanguage(), code)

	ctx := ASTContext{}
	ExtractDataForFile(tree.RootNode(), []byte(code), "auth.js", &ctx)

	for _, fn := range ctx.Functions {
		if fn.File != "auth.js" {
			t.Errorf("function %s file = %q, want auth.js", fn.Name, fn.File)
		}
	}
	for _, v := range ctx.Variables {
		if v.File != "auth.js" {
			t.Errorf("variable %s file = %q, want auth.js", v.Name, v.File)
		}
	}
}

func TestExtractData_PythonScopeAndSignature(t *testing.T) {
	code := `def outer(a, b):
    def inner(c):
        return c
    return inner(a)


class Engine:
    def charge(self, token):
        return token
`
	tree := parseWith(t, py.GetLanguage(), code)
	ctx := ASTContext{}
	ExtractData(tree.RootNode(), []byte(code), &ctx)

	outer, ok := findFunction(ctx.Functions, "outer")
	if !ok {
		t.Fatalf("outer not extracted: %#v", ctx.Functions)
	}
	if outer.Scope != "" {
		t.Errorf("outer scope = %q, want empty (module level)", outer.Scope)
	}

	inner, ok := findFunction(ctx.Functions, "inner")
	if !ok {
		t.Fatalf("inner not extracted: %#v", ctx.Functions)
	}
	if inner.Scope != "outer" {
		t.Errorf("nested function scope = %q, want outer", inner.Scope)
	}

	engine, ok := findFunction(ctx.Functions, "Engine")
	if !ok {
		t.Fatalf("Engine class not extracted")
	}
	if engine.Scope != "" {
		t.Errorf("Engine scope = %q, want empty", engine.Scope)
	}

	charge, ok := findFunction(ctx.Functions, "charge")
	if !ok {
		t.Fatalf("charge method not extracted: %#v", ctx.Functions)
	}
	if charge.Scope != "Engine" {
		t.Errorf("method scope = %q, want Engine", charge.Scope)
	}
	if charge.Signature == "" {
		t.Errorf("method signature should be captured")
	}
}

func TestExtractData_JavaScriptSignatureForFunctionAndArrow(t *testing.T) {
	code := "function add(a, b) { return a + b; }\nconst mul = (x, y) => x * y;\n"
	tree := parseWith(t, js.GetLanguage(), code)
	ctx := ASTContext{}
	ExtractData(tree.RootNode(), []byte(code), &ctx)

	add, ok := findFunction(ctx.Functions, "add")
	if !ok {
		t.Fatalf("add not extracted: %#v", ctx.Functions)
	}
	if add.Signature != "(a, b)" {
		t.Errorf("add signature = %q, want (a, b)", add.Signature)
	}

	mul, ok := findFunction(ctx.Functions, "mul")
	if !ok {
		t.Fatalf("mul not extracted as function: %#v", ctx.Functions)
	}
	if mul.Signature == "" {
		t.Errorf("arrow function signature should be captured")
	}
}
