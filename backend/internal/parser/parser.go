package parser

import (
	"bytes"
	"io"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type CodeElement struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Line    int    `json:"line"`
	Content string `json:"content"`
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
	case "function_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			nextFunction = nameNode.Content(sourceCode)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name: nextFunction, Kind: "Function",
				Line: int(node.StartPoint().Row) + 1, Content: node.Content(sourceCode),
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
