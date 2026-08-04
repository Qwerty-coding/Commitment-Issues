package engine

import (
	"context"
	"fmt"
	"os"

	sitter "github.com/smacker/go-tree-sitter"
	rust "github.com/smacker/go-tree-sitter/rust"
)

// ValidateAndWrite validates LLM output syntax before saving it to disk.
func ValidateAndWrite(candidateCode, outputPath string) error {
	parser := sitter.NewParser()
	parser.SetLanguage(rust.GetLanguage())

	tree, err := parser.ParseCtx(context.TODO(), nil, []byte(candidateCode))
	if err != nil || tree.RootNode().HasError() {
		return fmt.Errorf("AST Syntax Guard failed: LLM output contained syntax errors")
	}

	return os.WriteFile(outputPath, []byte(candidateCode), 0644)
}
