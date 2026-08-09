// isolate builds contextual operation payloads that describe the surrounding scope of each change.
package engine

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// FindEnclosingFunction walks up the AST to identify the enclosing function or type scope.
func FindEnclosingFunction(node *sitter.Node, sourceCode []byte) string {
	for current := node; current != nil; current = current.Parent() {
		switch current.Type() {
		case "function_item", "function_declaration", "method_declaration":
			for i := 0; i < int(current.NamedChildCount()); i++ {
				child := current.NamedChild(i)
				if child.Type() == "identifier" || child.Type() == "name" {
					return fmt.Sprintf("fn %s", child.Content(sourceCode))
				}
			}
			return "fn [anonymous]"
		case "struct_item", "impl_item", "class_declaration":
			for i := 0; i < int(current.NamedChildCount()); i++ {
				child := current.NamedChild(i)
				if child.Type() == "type_identifier" || child.Type() == "name" {
					return fmt.Sprintf("type %s", child.Content(sourceCode))
				}
			}
		}
	}
	return "Global Scope"
}

// BuildContextualOperations creates a compact payload with scope information for each change.
func BuildContextualOperations(localNode, remoteNode *sitter.Node, localSrc, remoteSrc []byte, action string) ASTOperation {
	scope := "Global Scope"
	localSnippet := ""
	remoteSnippet := ""

	if localNode != nil {
		scope = FindEnclosingFunction(localNode, localSrc)
		localSnippet = localNode.Content(localSrc)
	}
	if remoteNode != nil {
		if scope == "Global Scope" {
			scope = FindEnclosingFunction(remoteNode, remoteSrc)
		}
		remoteSnippet = remoteNode.Content(remoteSrc)
	}

	return ASTOperation{Action: action, EnclosingBlock: scope, LocalCode: localSnippet, RemoteCode: remoteSnippet}
}
