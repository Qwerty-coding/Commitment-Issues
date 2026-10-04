package parser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"CommitIssues/internal/fileutil"

	sitter "github.com/smacker/go-tree-sitter"
	bash "github.com/smacker/go-tree-sitter/bash"
	c "github.com/smacker/go-tree-sitter/c"
	cpp "github.com/smacker/go-tree-sitter/cpp"
	css "github.com/smacker/go-tree-sitter/css"
	csharp "github.com/smacker/go-tree-sitter/csharp"
	dockerfile "github.com/smacker/go-tree-sitter/dockerfile"
	golang "github.com/smacker/go-tree-sitter/golang"
	html "github.com/smacker/go-tree-sitter/html"
	java "github.com/smacker/go-tree-sitter/java"
	js "github.com/smacker/go-tree-sitter/javascript"
	kotlin "github.com/smacker/go-tree-sitter/kotlin"
	php "github.com/smacker/go-tree-sitter/php"
	py "github.com/smacker/go-tree-sitter/python"
	ruby "github.com/smacker/go-tree-sitter/ruby"
	rust "github.com/smacker/go-tree-sitter/rust"
	swift "github.com/smacker/go-tree-sitter/swift"
	toml "github.com/smacker/go-tree-sitter/toml"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
	yaml "github.com/smacker/go-tree-sitter/yaml"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var (
	// ErrFileTooLarge is returned when a source file exceeds the 10 MB limit.
	ErrFileTooLarge = errors.New("file exceeds maximum supported size for AST parsing")
	// ErrBinaryFile is returned when a file contains null bytes in its header.
	ErrBinaryFile = errors.New("file is binary; cannot parse as source code")
)

// Diagnostic represents a syntax error or parser warning located in source text.
type Diagnostic struct {
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"` // "error", "warning"
}

// CodeElement represents a syntactic declaration: function, class, method,
// constructor, interface, struct, variable, or import.
type CodeElement struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Line      int      `json:"line"`
	EndLine   int      `json:"endLine,omitempty"`
	Content   string   `json:"content"`
	Calls     []string `json:"calls,omitempty"`
	File      string   `json:"file,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	Signature string   `json:"signature,omitempty"`
}

// CurrentParserVersion is the active Tree-Sitter extraction parser version.
const CurrentParserVersion = "1.0.0"

// CurrentSchemaVersion is the schema version for extracted ASTContext.
const CurrentSchemaVersion = "1.0.0"

// ASTContext represents extracted symbols, imports, and parser diagnostics
// from an analyzed file.
type ASTContext struct {
	Functions   []CodeElement `json:"functions"`
	Variables   []CodeElement `json:"variables"`
	Imports     []CodeElement `json:"imports,omitempty"`
	Language    string        `json:"language,omitempty"`
	ParserName  string        `json:"parserName,omitempty"`
	ParserVer   string        `json:"parserVersion,omitempty"`
	Diagnostics []Diagnostic  `json:"diagnostics,omitempty"`
	ContentHash string        `json:"contentHash,omitempty"`
}

// Clone returns a deep copy of ASTContext to guarantee caller immutability.
func (ctx ASTContext) Clone() ASTContext {
	out := ctx
	if ctx.Functions != nil {
		out.Functions = make([]CodeElement, len(ctx.Functions))
		for i, fn := range ctx.Functions {
			out.Functions[i] = fn
			if fn.Calls != nil {
				out.Functions[i].Calls = append([]string(nil), fn.Calls...)
			}
		}
	}
	if ctx.Variables != nil {
		out.Variables = make([]CodeElement, len(ctx.Variables))
		for i, v := range ctx.Variables {
			out.Variables[i] = v
			if v.Calls != nil {
				out.Variables[i].Calls = append([]string(nil), v.Calls...)
			}
		}
	}
	if ctx.Imports != nil {
		out.Imports = make([]CodeElement, len(ctx.Imports))
		for i, imp := range ctx.Imports {
			out.Imports[i] = imp
			if imp.Calls != nil {
				out.Imports[i].Calls = append([]string(nil), imp.Calls...)
			}
		}
	}
	if ctx.Diagnostics != nil {
		out.Diagnostics = make([]Diagnostic, len(ctx.Diagnostics))
		copy(out.Diagnostics, ctx.Diagnostics)
	}
	return out
}

// NormalizeUTF8 removes UTF-8 BOM if present and ensures clean byte slice.
func NormalizeUTF8(input []byte) []byte {
	reader := transform.NewReader(bytes.NewReader(input), unicode.BOMOverride(transform.Nop))
	if out, err := io.ReadAll(reader); err == nil {
		return out
	}
	return input
}

// GetLanguageName returns the canonical language name for filename, or "" if unsupported.
func GetLanguageName(filename string) string {
	base := strings.ToLower(filepath.Base(filename))
	// Some formats (Dockerfile) have no extension and are recognised by name.
	if base == "dockerfile" || base == "containerfile" {
		return "dockerfile"
	}
	ext := strings.ToLower(filepath.Ext(filename))
	for name, info := range LanguageRegistry {
		for _, e := range info.FileExtensions {
			if e == ext {
				return name
			}
		}
	}
	return ""
}

// GetLanguageTier returns the support tier (1, 2, 3, or TierUnsupported).
func GetLanguageTier(filename string) Tier {
	name := GetLanguageName(filename)
	if name == "" {
		return TierUnsupported
	}
	return LanguageRegistry[name].Tier
}

// IsSupportedLanguage reports whether filename has a supported grammar parser.
func IsSupportedLanguage(filename string) bool {
	return GetLanguageForFile(filename) != nil
}

// GetLanguageForFile returns the Tree-Sitter language grammar for filename,
// or nil if unsupported.
func GetLanguageForFile(filename string) *sitter.Language {
	name := GetLanguageName(filename)
	switch name {
	case "python":
		return py.GetLanguage()
	case "go":
		return golang.GetLanguage()
	case "javascript":
		return js.GetLanguage()
	case "typescript":
		return typescript.GetLanguage()
	case "tsx":
		return tsx.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "c":
		return c.GetLanguage()
	case "cpp":
		return cpp.GetLanguage()
	case "csharp":
		return csharp.GetLanguage()
	case "php":
		return php.GetLanguage()
	case "ruby":
		return ruby.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "html":
		return html.GetLanguage()
	case "kotlin":
		return kotlin.GetLanguage()
	case "swift":
		return swift.GetLanguage()
	case "bash":
		return bash.GetLanguage()
	case "toml":
		return toml.GetLanguage()
	case "yaml":
		return yaml.GetLanguage()
	case "css":
		return css.GetLanguage()
	case "dockerfile":
		return dockerfile.GetLanguage()
	default:
		return nil
	}
}

// ParseAndCheckSyntax parses source using the grammar for filename.
// If the language is supported and has syntax errors, an error is returned.
// If the language is unsupported, it returns nil (fallback to text).
func ParseAndCheckSyntax(filename string, source []byte) error {
	lang := GetLanguageForFile(filename)
	if lang == nil {
		return nil
	}
	p := sitter.NewParser()
	p.SetLanguage(lang)
	tree, err := p.ParseCtx(context.Background(), nil, NormalizeUTF8(source))
	if err != nil {
		return err
	}
	if tree == nil {
		return fmt.Errorf("failed to parse syntax for %s", filename)
	}
	root := tree.RootNode()
	if root != nil && root.HasError() {
		return fmt.Errorf("syntax error in %s", filename)
	}
	return nil
}

// HashSource returns the hex-encoded SHA-256 hash of normalized source bytes.
func HashSource(source []byte) string {
	sum := sha256.Sum256(NormalizeUTF8(source))
	return hex.EncodeToString(sum[:])
}

// ParseAndExtract parses source using the grammar for filename, running file-size
// and binary safeguards first. It populates elements, scopes, signatures, imports,
// calls, and diagnostics.
//
// For unsupported files, it returns an ASTContext with Language: "unsupported"
// and a diagnostic indicating text fallback, with a nil error.
func ParseAndExtract(filename string, source []byte) (ASTContext, error) {
	contentHash := HashSource(source)
	if len(source) > fileutil.MaxFileSize {
		return ASTContext{
			Language:    "unsupported",
			ContentHash: contentHash,
			Diagnostics: []Diagnostic{{
				Message:  fmt.Sprintf("file exceeds maximum supported size (%d bytes)", fileutil.MaxFileSize),
				Severity: "error",
			}},
		}, ErrFileTooLarge
	}
	if fileutil.IsBinaryContent(source) {
		return ASTContext{
			Language:    "unsupported",
			ContentHash: contentHash,
			Diagnostics: []Diagnostic{{
				Message:  "binary file detected; parsing skipped",
				Severity: "error",
			}},
		}, ErrBinaryFile
	}

	lang := GetLanguageForFile(filename)
	langName := GetLanguageName(filename)
	if lang == nil {
		return ASTContext{
			Language:    "unsupported",
			ContentHash: contentHash,
			Diagnostics: []Diagnostic{{
				Message:  fmt.Sprintf("unsupported file extension %q; text fallback applied", filepath.Ext(filename)),
				Severity: "warning",
			}},
		}, nil
	}

	cleanSource := NormalizeUTF8(source)
	p := sitter.NewParser()
	p.SetLanguage(lang)
	tree, err := p.ParseCtx(context.Background(), nil, cleanSource)
	if err != nil {
		return ASTContext{
			Language:    langName,
			ContentHash: contentHash,
			Diagnostics: []Diagnostic{{
				Message:  fmt.Sprintf("parser failed: %v", err),
				Severity: "error",
			}},
		}, err
	}
	if tree == nil {
		return ASTContext{
			Language:    langName,
			ContentHash: contentHash,
			Diagnostics: []Diagnostic{{
				Message:  "failed to generate parse tree",
				Severity: "error",
			}},
		}, fmt.Errorf("failed to parse syntax for %s", filename)
	}

	ctx := ASTContext{
		Functions:   make([]CodeElement, 0),
		Variables:   make([]CodeElement, 0),
		Imports:     make([]CodeElement, 0),
		Language:    langName,
		ParserName:  "tree-sitter-" + langName,
		ParserVer:   CurrentParserVersion,
		ContentHash: contentHash,
	}

	ExtractDataForFile(tree.RootNode(), cleanSource, filename, &ctx)
	collectDiagnostics(tree.RootNode(), cleanSource, &ctx.Diagnostics)
	return ctx, nil
}

// ExtractPatternIdentifiers extracts variable names from destructured patterns.
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

// IsFunctionLike reports whether a grammar node is a function or arrow expression.
func IsFunctionLike(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	t := node.Type()
	return t == "arrow_function" || t == "function" || t == "function_expression"
}

// ExtractData extracts functions, classes, and variables from a parsed tree.
func ExtractData(node *sitter.Node, sourceCode []byte, contextData *ASTContext) {
	extractLanguageData(node, sourceCode, contextData, "", "", "")
}

// ExtractDataForFile extracts functions, classes, variables, and imports,
// annotating File on every element.
func ExtractDataForFile(node *sitter.Node, sourceCode []byte, file string, contextData *ASTContext) {
	langName := GetLanguageName(file)
	if contextData.Language == "" {
		contextData.Language = langName
		if langName != "" {
			contextData.ParserName = "tree-sitter-" + langName
			contextData.ParserVer = "1.0.0"
		}
	}
	extractLanguageData(node, sourceCode, contextData, "", "", langName)
	for i := range contextData.Functions {
		contextData.Functions[i].File = file
	}
	for i := range contextData.Variables {
		contextData.Variables[i].File = file
	}
	for i := range contextData.Imports {
		contextData.Imports[i].File = file
	}
}

func signatureOf(node *sitter.Node, sourceCode []byte) string {
	if node == nil {
		return ""
	}
	if params := node.ChildByFieldName("parameters"); params != nil {
		return params.Content(sourceCode)
	}
	return ""
}

func joinScope(parent, name string) string {
	switch {
	case parent == "":
		return name
	case name == "":
		return parent
	default:
		return parent + "." + name
	}
}
