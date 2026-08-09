// gaurd validates generated code syntax before writing the resolved output to disk.
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// ValidateAndWrite parses the generated code and writes it only when the syntax check passes.
func ValidateAndWrite(candidateCode, outputPath string) error {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(outputPath)))
	if ext == "" {
		return fmt.Errorf("output path must include a file extension")
	}

	language, err := GetLanguageForFile(outputPath)
	if err != nil {
		return err
	}

	parser := sitter.NewParser()
	parser.SetLanguage(language)

	tree, err := parser.ParseCtx(context.TODO(), nil, []byte(candidateCode))
	if err != nil || tree.RootNode().HasError() {
		return fmt.Errorf("AST syntax guard failed: generated code contains syntax errors")
	}

	return os.WriteFile(outputPath, []byte(candidateCode), 0644)
}
