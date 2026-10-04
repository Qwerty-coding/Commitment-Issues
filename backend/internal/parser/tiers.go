package parser

// Tier defines the level of parser and semantic support for a language.
type Tier int

const (
	// TierUnsupported represents languages with no Tree-Sitter grammar.
	// They fall back to text-diff analysis.
	TierUnsupported Tier = 0

	// Tier1 represents primary languages: full AST extraction, scopes,
	// signatures, variables, imports, calls, and conflict analysis.
	Tier1 Tier = 1

	// Tier2 represents major compiled/enterprise languages with AST extraction,
	// classes, methods, constructors, variables, imports, and calls.
	Tier2 Tier = 2

	// Tier3 represents scripting, systems, and markup languages with AST extraction.
	Tier3 Tier = 3

	// Tier5 represents extended languages and data/config formats whose
	// grammars ship natively with the pinned tree-sitter module. Extraction is
	// best-effort: declarations that match the shared node rules are captured,
	// otherwise the file still parses without falling back to text-only mode.
	// Languages whose grammars are NOT bundled (Dart, SQL, JSON) stay
	// TierUnsupported deliberately — see the Tier-5 decision notes.
	Tier5 Tier = 5
)

// LanguageInfo documents the capabilities and metadata of a language parser.
type LanguageInfo struct {
	Name            string   `json:"name"`
	Tier            Tier     `json:"tier"`
	GrammarPackage  string   `json:"grammarPackage"`
	FileExtensions  []string `json:"fileExtensions"`
	SupportsScope   bool     `json:"supportsScope"`
	SupportsSig     bool     `json:"supportsSignature"`
	SupportsVars    bool     `json:"supportsVariables"`
	SupportsImports bool     `json:"supportsImports"`
	SupportsCalls   bool     `json:"supportsCalls"`
	SupportsRanges  bool     `json:"supportsRanges"`
}

// LanguageRegistry maps canonical language names to their capability profiles.
var LanguageRegistry = map[string]LanguageInfo{
	// ─── Tier 1: Primary Languages ──────────────────────────────────────────
	"python": {
		Name:            "python",
		Tier:            Tier1,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/python",
		FileExtensions:  []string{".py", ".pyi", ".pyw"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"javascript": {
		Name:            "javascript",
		Tier:            Tier1,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/javascript",
		FileExtensions:  []string{".js", ".jsx", ".mjs", ".cjs"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"typescript": {
		Name:            "typescript",
		Tier:            Tier1,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/typescript/typescript",
		FileExtensions:  []string{".ts", ".mts", ".cts"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"tsx": {
		Name:            "tsx",
		Tier:            Tier1,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/typescript/tsx",
		FileExtensions:  []string{".tsx"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"go": {
		Name:            "go",
		Tier:            Tier1,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/golang",
		FileExtensions:  []string{".go"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},

	// ─── Tier 2: Enterprise & Systems Languages ──────────────────────────────
	"java": {
		Name:            "java",
		Tier:            Tier2,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/java",
		FileExtensions:  []string{".java"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"csharp": {
		Name:            "csharp",
		Tier:            Tier2,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/csharp",
		FileExtensions:  []string{".cs"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"c": {
		Name:            "c",
		Tier:            Tier2,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/c",
		FileExtensions:  []string{".c", ".h"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"cpp": {
		Name:            "cpp",
		Tier:            Tier2,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/cpp",
		FileExtensions:  []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},

	// ─── Tier 3: Systems, Scripting & Markup ─────────────────────────────────
	"rust": {
		Name:            "rust",
		Tier:            Tier3,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/rust",
		FileExtensions:  []string{".rs"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"ruby": {
		Name:            "ruby",
		Tier:            Tier3,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/ruby",
		FileExtensions:  []string{".rb", ".rake"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"php": {
		Name:            "php",
		Tier:            Tier3,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/php",
		FileExtensions:  []string{".php", ".phtml", ".php4", ".php5", ".php7"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"html": {
		Name:            "html",
		Tier:            Tier3,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/html",
		FileExtensions:  []string{".html", ".htm"},
		SupportsScope:   false,
		SupportsSig:     false,
		SupportsVars:    false,
		SupportsImports: false,
		SupportsCalls:   false,
		SupportsRanges:  true,
	},

	// ─── Tier 5: Extended languages & data/config formats ────────────────────
	"kotlin": {
		Name:            "kotlin",
		Tier:            Tier5,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/kotlin",
		FileExtensions:  []string{".kt", ".kts"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"swift": {
		Name:            "swift",
		Tier:            Tier5,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/swift",
		FileExtensions:  []string{".swift"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: true,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"bash": {
		Name:            "bash",
		Tier:            Tier5,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/bash",
		FileExtensions:  []string{".sh", ".bash"},
		SupportsScope:   true,
		SupportsSig:     true,
		SupportsVars:    true,
		SupportsImports: false,
		SupportsCalls:   true,
		SupportsRanges:  true,
	},
	"toml": {
		Name:            "toml",
		Tier:            Tier5,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/toml",
		FileExtensions:  []string{".toml"},
		SupportsScope:   false,
		SupportsSig:     false,
		SupportsVars:    true,
		SupportsImports: false,
		SupportsCalls:   false,
		SupportsRanges:  true,
	},
	"yaml": {
		Name:            "yaml",
		Tier:            Tier5,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/yaml",
		FileExtensions:  []string{".yaml", ".yml"},
		SupportsScope:   false,
		SupportsSig:     false,
		SupportsVars:    true,
		SupportsImports: false,
		SupportsCalls:   false,
		SupportsRanges:  true,
	},
	"css": {
		Name:            "css",
		Tier:            Tier5,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/css",
		FileExtensions:  []string{".css"},
		SupportsScope:   false,
		SupportsSig:     false,
		SupportsVars:    false,
		SupportsImports: false,
		SupportsCalls:   false,
		SupportsRanges:  true,
	},
	// Dockerfiles have no file extension; they are matched by base name in
	// GetLanguageName rather than through FileExtensions.
	"dockerfile": {
		Name:            "dockerfile",
		Tier:            Tier5,
		GrammarPackage:  "github.com/smacker/go-tree-sitter/dockerfile",
		FileExtensions:  []string{},
		SupportsScope:   false,
		SupportsSig:     false,
		SupportsVars:    false,
		SupportsImports: false,
		SupportsCalls:   false,
		SupportsRanges:  true,
	},
}
