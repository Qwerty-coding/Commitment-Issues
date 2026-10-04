# Module: Parser

## Purpose

The parser package is the AST foundation of the whole system. It wraps
Tree-sitter (cgo, 13 grammars) to turn raw file bytes into a structured,
versioned `ASTContext` — functions, variables, imports, calls, scopes,
signatures, and syntax diagnostics — plus the language-selection and
safeguard logic (size/binary/unsupported guards) that every other module
relies on. It is pure computation: no Git, no I/O beyond reading its input
bytes, no package-level mutable state.

## Files

### `backend/internal/parser/parser.go`

#### Primary Role
Public API of the package: type definitions, language/grammar selection,
source normalization and hashing, and the main entry point
`ParseAndExtract`. Also hosts the legacy single-tree extraction helpers.

#### Key Structures
- `Diagnostic{Message, Line, Column, Severity}` — a located syntax
  error/warning ("error" | "warning").
- `CodeElement{Name, Kind, Line, EndLine, Content, Calls, File, Scope,
  Signature}` — one extracted declaration (function, method, class,
  variable, import…). `File` is stamped by `ExtractDataForFile`, `Scope`
  is the dotted enclosing path, `Signature` is the parameter list text.
- `ASTContext{Functions, Variables, Imports []CodeElement, Language,
  ParserName, ParserVer, Diagnostics []Diagnostic, ContentHash}` — the
  per-file extraction result.
- Constants `CurrentParserVersion = "1.0.0"`, `CurrentSchemaVersion =
  "1.0.0"` — stamped into contexts and used as cache-key components.
- Sentinels `ErrFileTooLarge`, `ErrBinaryFile` — returned alongside an
  `ASTContext` that still carries `Language: "unsupported"`, the content
  hash, and an error `Diagnostic`, so callers can report *why*.
- Functions: `Clone()` (deep copy incl. per-element `Calls` — callers
  never mutate cached entries), `NormalizeUTF8` (strips UTF-8 BOM via
  `golang.org/x/text` `BOMOverride`), `HashSource` (SHA-256 hex of
  normalized bytes), `GetLanguageName` / `GetLanguageTier` /
  `IsSupportedLanguage` / `GetLanguageForFile` (extension → registry →
  grammar switch), `ParseAndCheckSyntax` (parse-only gate; unsupported
  language returns `nil`), `ParseAndExtract`, `ExtractData`,
  `ExtractDataForFile`, `ExtractPatternIdentifiers` (recursively finds
  identifiers in destructuring `array_pattern`/`object_pattern`/
  `assignment_pattern` nodes), `IsFunctionLike` (`arrow_function`,
  `function`, `function_expression`), `signatureOf` (the `parameters`
  child node's text), `joinScope` (dotted `parent.child` join).

#### Algorithms & Logic
`ParseAndExtract(filename, source)` runs a fixed pipeline:
1. `HashSource` — hash computed *first* so error contexts still carry it.
2. Size guard: `len(source) > fileutil.MaxFileSize` → `ErrFileTooLarge`
   with an error diagnostic (never parses huge files).
3. Binary guard: `fileutil.IsBinaryContent` (null-byte sniff) →
   `ErrBinaryFile` with an error diagnostic.
4. Unsupported extension → `ASTContext{Language: "unsupported"}` + a
   *warning* diagnostic + `nil` error — this is the deliberate "text
   fallback" path, not a failure.
5. Grammar selected by extension, `sitter.Parser.ParseCtx`, then
   `ExtractDataForFile` (extraction) + `collectDiagnostics` (ERROR/MISSING
   node walk). Parse failure yields an error diagnostic *and* the error.

`ExtractDataForFile` fills `Language`/`ParserName`/`ParserVer` when empty,
delegates to `extractLanguageData`, then stamps `File` on every element —
so downstream identity keys (`file+scope+kind+name+signature`) are stable.
Note the signature is `(filename string, source []byte)`: repo/path context
is added by `internal/cache`, which owns caching.

#### Wiring (Dependencies)
- Imports: `CommitIssues/internal/fileutil` (MaxFileSize, IsBinaryContent),
  `github.com/smacker/go-tree-sitter` + 13 grammar packages,
  `golang.org/x/text` (BOM normalization), stdlib (sha256, filepath…).
- Imported by: `internal/cache` (calls `ParseAndExtract`),
  `internal/semantic` (`CodeElement`, `ASTContext` types),
  `internal/context`, `internal/prompt`, `internal/report`,
  `internal/graph`, `internal/suggestions`, `internal/patch`,
  `internal/compare`, `internal/engine`, `internal/api` — all consume the
  types; only `cache` invokes the parser directly.

### `backend/internal/parser/extract.go`

#### Primary Role
The single recursive extractor that covers all Tier 1–3 languages:
`extractLanguageData(node, sourceCode, contextData, scope, currentFunction, lang)`.

#### Key Structures
- `extractLanguageData` — walks the Tree-sitter tree node by node
  (~449 lines of a large `switch nodeType`).
- Name helpers: `extractFunctionName`, `extractMethodName`,
  `extractNodeFieldOrChild(field, …)`, `extractGoReceiverType` (Go
  receiver → scope), `extractCDeclaratorName` (C declarator indirection),
  `extractCallName` (callee text incl. member calls).
- `recordCallOnFunction` — attaches a call to the innermost enclosing
  function element.
- `collectDiagnostics(node, source, *[]Diagnostic)` — recursive walk that
  records Tree-sitter `ERROR`/`MISSING` nodes as located diagnostics.

#### Algorithms & Logic
- One recursive pass threads `scope`/`currentFunction` down the tree; on
  each declaration node it appends a `CodeElement` with line range,
  content, scope, and signature, then recurses with an updated scope
  (`joinScope(scope, name)`).
- Language-conditional classification: a `function_declaration` inside a
  scope becomes `Kind: "Method"` for `python`, `rust`, `php` (their
  grammar uses function nodes for methods); Go receivers contribute to
  scope via `extractGoReceiverType`, so `(*Server).Handle` gets its own
  scope instead of colliding with a free function.
- Calls are recorded only onto the enclosing function, and imports,
  variables, classes/structs/interfaces/enums are matched by node type
  per grammar family (JS/TS/TSX, Python, Go, Java, C, C++, C#, PHP,
  Ruby, Rust, HTML — HTML additionally extracts `Elements`).
- Diagnostics are collected in the same walk family so a file with syntax
  errors still yields partial symbols plus precise error locations.

#### Wiring (Dependencies)
- Imports: stdlib `strings` + `sitter` only (pure tree logic).
- Imported by: `parser.go` (same package) — called from
  `ParseAndExtract`/`ExtractData`/`ExtractDataForFile`; no other package
  can reach it (lowercase).

### `backend/internal/parser/tiers.go`

#### Primary Role
Declarative language capability registry: which languages are supported,
their tier, extensions, grammar package, and feature flags.

#### Key Structures
- `type Tier int` with constants `TierUnsupported = 0` (text fallback),
  `Tier1` (primary: full extraction — Python, JavaScript, TypeScript,
  TSX, Go), `Tier2` (enterprise/compiled — Java, C#, C, C++), `Tier3`
  (scripting/systems/markup — Rust, Ruby, PHP, HTML), `Tier5` (extended
  languages & data/config formats whose grammars ship natively in the
  pinned tree-sitter module — Kotlin, Swift, Shell/bash, TOML, YAML, CSS
  and extension-less Dockerfiles matched by base name).
- **Tier-5 decision:** Dart, SQL and JSON grammars are NOT bundled with
  `smacker/go-tree-sitter@v0.0.0-20240827094217` and Markdown ships a
  bespoke two-parse-tree API, so all four are deliberately deferred to
  the text-diff path (pinned by `tier5_test.go`) rather than vendoring
  or upgrading the module.
- `LanguageInfo{Name, Tier, GrammarPackage, FileExtensions, SupportsScope,
  SupportsSig, SupportsVars, SupportsImports, SupportsCalls,
  SupportsRanges}` — capability profile per language.
- `LanguageRegistry map[string]LanguageInfo` — 21 entries keyed by
  canonical name; HTML and the data/config profiles (TOML/YAML/CSS/
  Dockerfile) carry `SupportsRanges`-only capability.

#### Algorithms & Logic
Pure data — but it is the single source of truth for extension lookup:
`GetLanguageName` linearly scans every entry's `FileExtensions`, so adding
a language means adding a registry entry *and* a `GetLanguageForFile`
switch case *and* an `extractLanguageData` branch.

#### Wiring (Dependencies)
- Imports: none.
- Imported by: `parser.go` (selection functions); consumed indirectly by
  everything that asks `IsSupportedLanguage`/`GetLanguageTier`
  (`engine`, `compare`, `report`, API metadata).

## Cross-Module Flow

- `internal/cache.GetOrParse` → `ParseAndExtract` → immutable cached
  `ASTContext` (keyed by repo/path/content-hash/parser+schema versions) →
  cloned out to callers.
- `engine.ProcessConflictFile` extracts Base/Ours/Theirs (3 ×
  `ASTContext`, or text-fallback diagnostics) →
  `semantic.GenerateSmartDiffContext` classifies symbols →
  `promptcontext.BuildPromptContext` scopes functions → AI payload.
- Unsupported/oversized/binary files degrade gracefully: `Language:
  "unsupported"` + diagnostic, pipeline continues with text diff.
- `internal/compare` calls `IsSupportedLanguage` and treats
  `ErrFileTooLarge` as a typed `analysis_error` rather than a crash.

## Tests

- `backend/internal/parser/parser_test.go` — per-language extraction
  correctness (`TestParseAndExtract_Tier1_Go/TypeScript`,
  `Tier2_Java/CSharp/CPP`, `Tier3_Rust/Ruby/PHP/HTML`), `File` stamped on
  every element, Python scope+signature, JS function/arrow signatures,
  safeguards (size/binary/unsupported), diagnostics on syntax errors
  (`TestParseAndExtract_DiagnosticsOnSyntaxError`).
