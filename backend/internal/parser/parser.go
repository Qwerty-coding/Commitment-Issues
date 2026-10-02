package parser

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	c "github.com/smacker/go-tree-sitter/c"
	cpp "github.com/smacker/go-tree-sitter/cpp"
	csharp "github.com/smacker/go-tree-sitter/csharp"
	golang "github.com/smacker/go-tree-sitter/golang"
	html "github.com/smacker/go-tree-sitter/html"
	java "github.com/smacker/go-tree-sitter/java"
	js "github.com/smacker/go-tree-sitter/javascript"
	php "github.com/smacker/go-tree-sitter/php"
	py "github.com/smacker/go-tree-sitter/python"
	ruby "github.com/smacker/go-tree-sitter/ruby"
	rust "github.com/smacker/go-tree-sitter/rust"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type CodeElement struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Line    int      `json:"line"`
	Content string   `json:"content"`
	Calls   []string `json:"calls,omitempty"`
	// File is the source file the element was extracted from.
	File string `json:"file,omitempty"`
	// Scope is the dotted path of enclosing named scopes (e.g. "OrderEngine.charge").
	Scope string `json:"scope,omitempty"`
	// Signature is the raw parameter list text, used to distinguish overloads.
	Signature string `json:"signature,omitempty"`
}

type ASTContext struct {
	Functions []CodeElement `json:"functions"`
	Variables []CodeElement `json:"variables"`
}

func NormalizeUTF8(input []byte) []byte {
	reader := transform.NewReader(bytes.NewReader(input), unicode.BOMOverride(transform.Nop))
	if out, err := io.ReadAll(reader); err == nil {
		return out
	}

	return input
}

func GetLanguageForFile(filename string) *sitter.Language {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".py":
		return py.GetLanguage()
	case ".go":
		return golang.GetLanguage()
	case ".js", ".jsx", ".mjs", ".cjs":
		return js.GetLanguage()
	case ".ts":
		return typescript.GetLanguage()
	case ".tsx":
		return tsx.GetLanguage()
	case ".java":
		return java.GetLanguage()
	case ".c":
		return c.GetLanguage()
	case ".cc", ".cpp", ".cxx", ".hpp":
		return cpp.GetLanguage()
	case ".cs":
		return csharp.GetLanguage()
	case ".php":
		return php.GetLanguage()
	case ".rb":
		return ruby.GetLanguage()
	case ".rs":
		return rust.GetLanguage()
	case ".html":
		return html.GetLanguage()
	default:
		return js.GetLanguage()
	}
}

func ExtractPatternIdentifiers(node *sitter.Node, sourceCode []byte, content string) []CodeElement {
	var elems []CodeElement
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier", "shorthand_property_identifier_pattern":
			elems = append(elems, CodeElement{
				Name: child.Content(sourceCode), Kind: "Variable",
				Line: int(child.StartPoint().Row) + 1, Content: content,
			})
		case "assignment_pattern":
			if left := child.ChildByFieldName("left"); left != nil && left.Type() == "identifier" {
				elems = append(elems, CodeElement{
					Name: left.Content(sourceCode), Kind: "Variable",
					Line: int(left.StartPoint().Row) + 1, Content: content,
				})
			}
		case "array_pattern", "object_pattern":
			elems = append(elems, ExtractPatternIdentifiers(child, sourceCode, content)...)
		}
	}
	return elems
}

func IsFunctionLike(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	t := node.Type()
	return t == "arrow_function" || t == "function" || t == "function_expression"
}

// ExtractData extracts functions and variables from a parsed tree. File, scope
// and signature metadata are also populated where the grammar exposes them.
func ExtractData(node *sitter.Node, sourceCode []byte, contextData *ASTContext) {
	extractData(node, sourceCode, contextData, "", "")
}

// ExtractDataForFile is ExtractData plus a file label applied to every element,
// so symbol identities can be scoped to a file.
func ExtractDataForFile(node *sitter.Node, sourceCode []byte, file string, contextData *ASTContext) {
	extractData(node, sourceCode, contextData, "", "")
	for i := range contextData.Functions {
		contextData.Functions[i].File = file
	}
	for i := range contextData.Variables {
		contextData.Variables[i].File = file
	}
}

func signatureOf(node *sitter.Node, sourceCode []byte) string {
	if node == nil {
		return ""
	}
	if params := node.ChildByFieldName("parameters"); params != nil {
		return params.Content(sourceCode)
	}
	return ""
}

func joinScope(parent, name string) string {
	switch {
	case parent == "":
		return name
	case name == "":
		return parent
	default:
		return parent + "." + name
	}
}

func extractData(node *sitter.Node, sourceCode []byte, contextData *ASTContext, scope, currentFunction string) {
	if node == nil {
		return
	}

	nextFunction := currentFunction
	nextScope := scope

	switch node.Type() {
	case "function_declaration", "function_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name := nameNode.Content(sourceCode)
			nextFunction = name
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:      name,
				Kind:      "Function",
				Line:      int(node.StartPoint().Row) + 1,
				Content:   node.Content(sourceCode),
				Scope:     scope,
				Signature: signatureOf(node, sourceCode),
			})
		}
	case "class_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name := nameNode.Content(sourceCode)
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name: name, Kind: "Class",
				Line:    int(node.StartPoint().Row) + 1,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "assignment":
		if left := node.ChildByFieldName("left"); left != nil && left.Type() == "identifier" {
			contextData.Variables = append(contextData.Variables, CodeElement{
				Name: left.Content(sourceCode), Kind: "Variable",
				Line: int(left.StartPoint().Row) + 1, Content: left.Content(sourceCode),
				Scope: scope,
			})
		}

	case "lexical_declaration", "variable_declaration":
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() != "variable_declarator" {
				extractData(child, sourceCode, contextData, scope, currentFunction)
				continue
			}

			nameNode := child.ChildByFieldName("name")
			valueNode := child.ChildByFieldName("value")
			if nameNode == nil {
				continue
			}

			valueContent := ""
			if valueNode != nil {
				valueContent = valueNode.Content(sourceCode)
			}

			switch nameNode.Type() {
			case "identifier":
				line := int(nameNode.StartPoint().Row) + 1
				name := nameNode.Content(sourceCode)
				childFunction := currentFunction
				childScope := scope
				if IsFunctionLike(valueNode) {
					childFunction = name
					childScope = joinScope(scope, name)
					contextData.Functions = append(contextData.Functions, CodeElement{
						Name: name, Kind: "Function", Line: line, Content: valueContent,
						Scope: scope, Signature: signatureOf(valueNode, sourceCode),
					})
				} else {
					contextData.Variables = append(contextData.Variables, CodeElement{
						Name: name, Kind: "Variable", Line: line, Content: valueContent,
						Scope: scope,
					})
				}
				if valueNode != nil {
					extractData(valueNode, sourceCode, contextData, childScope, childFunction)
				}
			case "array_pattern", "object_pattern":
				identifiers := ExtractPatternIdentifiers(nameNode, sourceCode, valueContent)
				for i := range identifiers {
					identifiers[i].Scope = scope
				}
				contextData.Variables = append(contextData.Variables, identifiers...)
				if valueNode != nil {
					extractData(valueNode, sourceCode, contextData, scope, currentFunction)
				}
			}
		}
		return

	case "call_expression":
		if nextFunction != "" {
			callee := node.ChildByFieldName("function")
			if callee == nil && node.ChildCount() > 0 {
				callee = node.Child(0)
			}
			if callee != nil {
				callName := strings.TrimSpace(callee.Content(sourceCode))
				if callName != "" {
					for i := range contextData.Functions {
						if contextData.Functions[i].Kind == "Function" && contextData.Functions[i].Name == nextFunction {
							alreadyRecorded := false
							for _, existing := range contextData.Functions[i].Calls {
								if existing == callName {
									alreadyRecorded = true
									break
								}
							}
							if !alreadyRecorded {
								contextData.Functions[i].Calls = append(contextData.Functions[i].Calls, callName)
							}
							break
						}
					}
				}
			}
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		extractData(node.Child(i), sourceCode, contextData, nextScope, nextFunction)
	}
}
