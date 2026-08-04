package engine

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// FindEnclosingFunction walks up the AST to find the parent function signature.
func FindEnclosingFunction(node *sitter.Node, sourceCode []byte) string {
	current := node
	for current != nil {
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
		current = current.Parent()
	}
	return "Global Scope"
}

// BuildContextualOperations maps raw AST diffs into operation payloads with function context.
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

	return ASTOperation{
		Action:         action,
		EnclosingBlock: scope,
		LocalCode:      localSnippet,
		RemoteCode:     remoteSnippet,
	}
}
