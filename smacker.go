package main

import (
	"fmt"
	"os"
	"path/filepath"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/gum"
)

func getAST(filename string) (*sitter.Node, []byte) {
	sourceCode, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Failed to read %s: %v\n", filename, err)
		os.Exit(1)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(javascript.GetLanguage())

	tree := parser.Parse(nil, sourceCode)
	return tree.RootNode(), sourceCode
}

func converter(sitterNode *sitter.Node, sourceCode []byte) *gum.Tree {
	if sitterNode == nil {
		return nil
	}

	gumNode := &gum.Tree{
		Type:     sitterNode.Type(),
		Value:    sitterNode.Content(sourceCode), 
		Children: []*gum.Tree{},
	}

	
	childCount := sitterNode.ChildCount()
	for i := uint32(0); i < childCount; i++ {
		childSitterNode := sitterNode.Child(int(i))
		childGumNode := converter(childSitterNode, sourceCode)

		if childGumNode != nil {
			gumNode.Children = append(gumNode.Children, childGumNode)
		}
	}

	return gumNode
}

func runSmacker(base string){
		//indicating that engine is on.
		fmt.Println("Initializing AST Engine...")

		//generates the ASTs
		tsNode1, source1 := getAST(filepath.Join("testfiles", "ours"+base))
		tsNode2, source2 := getAST(filepath.Join("testfiles", "theirs"+base))
		fmt.Println("Successfully generated Tree-sitter ASTs.")

		gumTree1 := converter(tsNode1, source1)
		gumTree2 := converter(tsNode2, source2)

		gumTree1.Refresh()
		gumTree2.Refresh()
		fmt.Println("Successfully converted to GumTree format.")

		fmt.Println("\n--- Running Native Diff Engine ---")

		mapping := gum.Match(gumTree1, gumTree2)
		actions := gum.Patch(gumTree1, gumTree2, mapping)

		fmt.Printf("Total structural changes found: %d\n", len(actions))

		for _, action := range actions {
			fmt.Println(action.String())
		}
	}