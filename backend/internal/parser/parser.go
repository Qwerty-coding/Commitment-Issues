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

func ExtractData(node *sitter.Node, sourceCode []byte, contextData *ASTContext) {
	extractData(node, sourceCode, contextData, "")
}

func extractData(node *sitter.Node, sourceCode []byte, contextData *ASTContext, currentFunction string) {
	if node == nil {
		return
	}

	nextFunction := currentFunction

	switch node.Type() {
	case "function_declaration", "function_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			nextFunction = nameNode.Content(sourceCode)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name: nextFunction, Kind: "Function",
				Line: int(node.StartPoint().Row) + 1, Content: node.Content(sourceCode),
			})
		}
	case "class_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name: nameNode.Content(sourceCode), Kind: "Class",
				Line: int(node.StartPoint().Row) + 1, Content: node.Content(sourceCode),
			})
		}

	case "assignment":
		if left := node.ChildByFieldName("left"); left != nil && left.Type() == "identifier" {
			name := left.Content(sourceCode)
			contextData.Variables = append(contextData.Variables, CodeElement{
				Name: name, Kind: "Variable",
				Line: int(left.StartPoint().Row) + 1, Content: left.Content(sourceCode),
			})
		}

	case "lexical_declaration", "variable_declaration":
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() != "variable_declarator" {
				extractData(child, sourceCode, contextData, currentFunction)
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
				if IsFunctionLike(valueNode) {
					childFunction = name
					contextData.Functions = append(contextData.Functions, CodeElement{
						Name: name, Kind: "Function", Line: line, Content: valueContent,
					})
				} else {
					contextData.Variables = append(contextData.Variables, CodeElement{
						Name: name, Kind: "Variable", Line: line, Content: valueContent,
					})
				}
				if valueNode != nil {
					extractData(valueNode, sourceCode, contextData, childFunction)
				}
			case "array_pattern", "object_pattern":
				contextData.Variables = append(contextData.Variables, ExtractPatternIdentifiers(nameNode, sourceCode, valueContent)...)
				if valueNode != nil {
					extractData(valueNode, sourceCode, contextData, currentFunction)
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
		extractData(node.Child(i), sourceCode, contextData, nextFunction)
	}
}
