package engine

import (
	"context"
	"fmt"
	"os"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"
	// "github.com/smacker/gum" // Conceptual Gumtree library
)

// ParseFile reads a file and generates a Tree-Sitter AST
func ParseFile(ctx context.Context, filePath string) (*sitter.Tree, []byte, error) {

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}

	parser := sitter.NewParser()
	parser.SetLanguage(rust.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, nil, err
	}

	// Guard: Ensure initial files do not have syntax errors
	if tree.RootNode().HasError() {
		return nil, nil, fmt.Errorf("syntax error detected in %s before merge", filePath)
	}

	return tree, content, nil
}

// AdaptTreeSitterToGum maps a tree-sitter node to a GumTree structure
func AdaptTreeSitterToGum(node *sitter.Node, sourceCode []byte) interface{} /* *gum.Tree */ {
	if node == nil {
		return nil
	}
	// Conceptual implementation to convert structures for GumTree
	return nil
}

// BuildASTPayload parses base/local/remote files and builds a reduced AST payload.
// This draft compares named top-level items such as functions, structs, impls, and enums.
func BuildASTPayload(basePath, localPath, remotePath string) (ConflictPayload, error) {
	ctx := context.Background()

	baseTree, baseSrc, err := ParseFile(ctx, basePath)
	if err != nil {
		return ConflictPayload{}, fmt.Errorf("parse base file: %w", err)
	}

	localTree, localSrc, err := ParseFile(ctx, localPath)
	if err != nil {
		return ConflictPayload{}, fmt.Errorf("parse local file: %w", err)
	}

	remoteTree, remoteSrc, err := ParseFile(ctx, remotePath)
	if err != nil {
		return ConflictPayload{}, fmt.Errorf("parse remote file: %w", err)
	}

	payload := ConflictPayload{
		FilePath: localPath,
	}

	baseNodes := collectTopLevelNodes(baseTree.RootNode(), baseSrc)
	localNodes := collectTopLevelNodes(localTree.RootNode(), localSrc)
	remoteNodes := collectTopLevelNodes(remoteTree.RootNode(), remoteSrc)

	keySet := make(map[string]struct{})
	for key := range baseNodes {
		keySet[key] = struct{}{}
	}
	for key := range localNodes {
		keySet[key] = struct{}{}
	}
	for key := range remoteNodes {
		keySet[key] = struct{}{}
	}

	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		baseNode := baseNodes[key]
		localNode := localNodes[key]
		remoteNode := remoteNodes[key]

		baseCode := nodeContent(baseNode, baseSrc)
		localCode := nodeContent(localNode, localSrc)
		remoteCode := nodeContent(remoteNode, remoteSrc)

		if localCode == baseCode && remoteCode == baseCode {
			continue
		}

		switch {
		case localNode != nil && remoteNode != nil:
			if localCode != remoteCode {
				payload.Operations = append(
					payload.Operations,
					BuildContextualOperations(localNode, remoteNode, localSrc, remoteSrc, "UPDATE"),
				)
			}
		case localNode != nil && remoteNode == nil:
			payload.Operations = append(
				payload.Operations,
				BuildContextualOperations(localNode, nil, localSrc, nil, "INSERT"),
			)
		case localNode == nil && remoteNode != nil:
			payload.Operations = append(
				payload.Operations,
				BuildContextualOperations(nil, remoteNode, nil, remoteSrc, "DELETE"),
			)
		}
	}

	return payload, nil
}

func collectTopLevelNodes(root *sitter.Node, source []byte) map[string]*sitter.Node {
	nodes := make(map[string]*sitter.Node)
	if root == nil {
		return nodes
	}

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		key := topLevelKey(child, source)
		if key == "" {
			continue
		}
		if _, exists := nodes[key]; !exists {
			nodes[key] = child
		}
	}

	return nodes
}

func topLevelKey(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Type() {
	case "function_item", "function_declaration", "method_declaration":
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "identifier" || child.Type() == "name" {
				return "fn:" + child.Content(source)
			}
		}
		return "fn:anonymous"

	case "struct_item", "impl_item", "class_declaration", "enum_item", "trait_item":
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "type_identifier" || child.Type() == "name" {
				return node.Type() + ":" + child.Content(source)
			}
		}
	}

	return node.Type()
}

func nodeContent(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return node.Content(source)
}
