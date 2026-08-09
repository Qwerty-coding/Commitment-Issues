// languages maps file extensions to the appropriate Tree-sitter parsers.
package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/css"
	"github.com/smacker/go-tree-sitter/cue"
	"github.com/smacker/go-tree-sitter/dockerfile"
	"github.com/smacker/go-tree-sitter/elixir"
	"github.com/smacker/go-tree-sitter/elm"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/groovy"
	"github.com/smacker/go-tree-sitter/hcl"
	"github.com/smacker/go-tree-sitter/html"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/lua"
	markdown "github.com/smacker/go-tree-sitter/markdown/tree-sitter-markdown"
	"github.com/smacker/go-tree-sitter/ocaml"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/protobuf"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/sql"
	"github.com/smacker/go-tree-sitter/svelte"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/toml"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/yaml"
)

type languageSpec struct {
	extensions []string
	getLang    func() *sitter.Language
}

var languageSpecs = []languageSpec{
	{extensions: []string{".rs"}, getLang: rust.GetLanguage},
	{extensions: []string{".py"}, getLang: python.GetLanguage},
	{extensions: []string{".go"}, getLang: golang.GetLanguage},
	{extensions: []string{".js", ".jsx", ".mjs", ".cjs"}, getLang: javascript.GetLanguage},
	{extensions: []string{".ts"}, getLang: typescript.GetLanguage},
	{extensions: []string{".tsx"}, getLang: tsx.GetLanguage},
	{extensions: []string{".c", ".h"}, getLang: c.GetLanguage},
	{extensions: []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"}, getLang: cpp.GetLanguage},
	{extensions: []string{".cs"}, getLang: csharp.GetLanguage},
	{extensions: []string{".css", ".scss", ".sass"}, getLang: css.GetLanguage},
	{extensions: []string{".cue"}, getLang: cue.GetLanguage},
	{extensions: []string{".ex", ".exs"}, getLang: elixir.GetLanguage},
	{extensions: []string{".elm"}, getLang: elm.GetLanguage},
	{extensions: []string{".groovy", ".gradle"}, getLang: groovy.GetLanguage},
	{extensions: []string{".hcl"}, getLang: hcl.GetLanguage},
	{extensions: []string{".html", ".htm"}, getLang: html.GetLanguage},
	{extensions: []string{".java"}, getLang: java.GetLanguage},
	{extensions: []string{".kt", ".kts"}, getLang: kotlin.GetLanguage},
	{extensions: []string{".lua"}, getLang: lua.GetLanguage},
	{extensions: []string{".md", ".markdown"}, getLang: markdown.GetLanguage},
	{extensions: []string{".ml", ".mli"}, getLang: ocaml.GetLanguage},
	{extensions: []string{".php"}, getLang: php.GetLanguage},
	{extensions: []string{".proto"}, getLang: protobuf.GetLanguage},
	{extensions: []string{".rb"}, getLang: ruby.GetLanguage},
	{extensions: []string{".scala"}, getLang: scala.GetLanguage},
	{extensions: []string{".sql"}, getLang: sql.GetLanguage},
	{extensions: []string{".svelte"}, getLang: svelte.GetLanguage},
	{extensions: []string{".swift"}, getLang: swift.GetLanguage},
	{extensions: []string{".toml"}, getLang: toml.GetLanguage},
	{extensions: []string{".yaml", ".yml"}, getLang: yaml.GetLanguage},
	{extensions: []string{".sh", ".bash", ".zsh"}, getLang: bash.GetLanguage},
}

// GetLanguageForFile selects the right Tree-sitter parser for a file path.
func GetLanguageForFile(filePath string) (*sitter.Language, error) {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(filePath)))
	if ext == "" {
		base := strings.ToLower(filepath.Base(filePath))
		if base == "dockerfile" || base == "dockerfile.dev" {
			return dockerfile.GetLanguage(), nil
		}
		return nil, fmt.Errorf("unsupported file path: %s", filePath)
	}

	for _, spec := range languageSpecs {
		for _, candidate := range spec.extensions {
			if ext == candidate {
				return spec.getLang(), nil
			}
		}
	}
	return nil, fmt.Errorf("unsupported file extension: %s", ext)
}
