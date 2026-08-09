// ast handles parsing source files and building the AST-based conflict payload for resolution.
package engine

import (
	"context"
	"fmt"
	"os"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"
)

// ParseFile reads a source file and returns its parsed tree plus raw contents.
func ParseFile(ctx context.Context, filePath string) (*sitter.Tree, []byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}

	language, err := GetLanguageForFile(filePath)
	if err != nil {
		return nil, nil, err
	}

	parser := sitter.NewParser()
	parser.SetLanguage(language)

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, nil, err
	}
	if tree.RootNode().HasError() {
		return nil, nil, fmt.Errorf("syntax error detected in %s before merge", filePath)
	}

	return tree, content, nil
}

// BuildASTPayload compares the base/local/remote trees and creates a compact conflict payload.
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

	payload := ConflictPayload{FilePath: localPath}
	baseNodes := collectTopLevelNodes(baseTree.RootNode(), baseSrc)
	localNodes := collectTopLevelNodes(localTree.RootNode(), localSrc)
	remoteNodes := collectTopLevelNodes(remoteTree.RootNode(), remoteSrc)

	keys := make(map[string]struct{})
	for key := range baseNodes {
		keys[key] = struct{}{}
	}
	for key := range localNodes {
		keys[key] = struct{}{}
	}
	for key := range remoteNodes {
		keys[key] = struct{}{}
	}

	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)

	for _, key := range ordered {
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
		case localNode != nil && remoteNode != nil && localCode != remoteCode:
			payload.Operations = append(payload.Operations, BuildContextualOperations(localNode, remoteNode, localSrc, remoteSrc, "UPDATE"))
		case localNode != nil && remoteNode == nil:
			payload.Operations = append(payload.Operations, BuildContextualOperations(localNode, nil, localSrc, nil, "INSERT"))
		case localNode == nil && remoteNode != nil:
			payload.Operations = append(payload.Operations, BuildContextualOperations(nil, remoteNode, nil, remoteSrc, "DELETE"))
		}
	}

	return payload, nil
}

// collectTopLevelNodes gathers named top-level declarations for comparison.
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

// topLevelKey builds a stable identifier for a top-level declaration.
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

// nodeContent returns the source text for a node, or empty when the node is nil.
func nodeContent(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return node.Content(source)
}
