package parser

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// extractLanguageData is the comprehensive extractor covering Tier 1, Tier 2,
// and Tier 3 languages. It inspects grammar nodes according to the language rules
// and extracts functions, methods, constructors, classes, structs, interfaces,
// variables, imports, calls, qualified scopes, signatures, and line ranges.
func extractLanguageData(node *sitter.Node, sourceCode []byte, contextData *ASTContext, scope, currentFunction, lang string) {
	if node == nil {
		return
	}

	nextFunction := currentFunction
	nextScope := scope
	nodeType := node.Type()

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	switch nodeType {

	// ─── FUNCTIONS & METHODS ─────────────────────────────────────────────────
	case "function_declaration", "function_definition", "function_item":
		// Works across Go, JS/TS, Python, C/C++, Rust, PHP
		name := extractFunctionName(node, sourceCode)
		if name != "" {
			kind := "Function"
			if scope != "" && (lang == "python" || lang == "rust" || lang == "php") {
				kind = "Method"
			}
			nextFunction = name
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:      name,
				Kind:      kind,
				Line:      startLine,
				EndLine:   endLine,
				Content:   node.Content(sourceCode),
				Scope:     scope,
				Signature: signatureOf(node, sourceCode),
			})
		}

	case "method_declaration", "method_definition", "method":
		// Works across Go, Java, C#, JS/TS classes, PHP, Ruby
		name := extractMethodName(node, sourceCode)
		if name != "" {
			kind := "Method"
			methodScope := scope

			// In Go: extract receiver type as the method scope (e.g. "Server" or "*Server")
			if lang == "go" {
				if rcvr := node.ChildByFieldName("receiver"); rcvr != nil {
					rcvrType := extractGoReceiverType(rcvr, sourceCode)
					if rcvrType != "" {
						methodScope = rcvrType
					}
				}
			}

			// In JS/TS: constructor method
			if name == "constructor" {
				kind = "Constructor"
			}

			nextFunction = name
			nextScope = joinScope(methodScope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:      name,
				Kind:      kind,
				Line:      startLine,
				EndLine:   endLine,
				Content:   node.Content(sourceCode),
				Scope:     methodScope,
				Signature: signatureOf(node, sourceCode),
			})
		}

	case "singleton_method":
		// Ruby def self.method_name
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextFunction = name
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:      name,
				Kind:      "Method",
				Line:      startLine,
				EndLine:   endLine,
				Content:   node.Content(sourceCode),
				Scope:     scope,
				Signature: signatureOf(node, sourceCode),
			})
		}

	case "constructor_declaration":
		// Java, C#
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextFunction = name
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:      name,
				Kind:      "Constructor",
				Line:      startLine,
				EndLine:   endLine,
				Content:   node.Content(sourceCode),
				Scope:     scope,
				Signature: signatureOf(node, sourceCode),
			})
		}

	// ─── CLASSES, STRUCTS, INTERFACES, TRAITS, ENUMS ─────────────────────────
	case "class_definition", "class_declaration", "class":
		// Python, JS/TS, Java, C#, Ruby, PHP
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:    name,
				Kind:    "Class",
				Line:    startLine,
				EndLine: endLine,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "interface_declaration":
		// Java, C#, TypeScript, PHP
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:    name,
				Kind:    "Interface",
				Line:    startLine,
				EndLine: endLine,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "struct_declaration", "struct_item", "class_specifier", "struct_specifier":
		// C, C++, C#, Rust
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			kind := "Struct"
			if nodeType == "class_specifier" {
				kind = "Class"
			}
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:    name,
				Kind:    kind,
				Line:    startLine,
				EndLine: endLine,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "enum_declaration", "enum_item":
		// Java, C#, Rust
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:    name,
				Kind:    "Enum",
				Line:    startLine,
				EndLine: endLine,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "trait_declaration", "trait_item":
		// Rust, PHP
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:    name,
				Kind:    "Trait",
				Line:    startLine,
				EndLine: endLine,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "type_declaration":
		// Go type Foo struct / type Bar interface
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "type_spec" {
				typeName := extractNodeFieldOrChild(child, "name", sourceCode)
				if typeName != "" {
					typeKind := "Type"
					if tChild := child.ChildByFieldName("type"); tChild != nil {
						switch tChild.Type() {
						case "struct_type":
							typeKind = "Struct"
						case "interface_type":
							typeKind = "Interface"
						}
					}
					contextData.Functions = append(contextData.Functions, CodeElement{
						Name:    typeName,
						Kind:    typeKind,
						Line:    int(child.StartPoint().Row) + 1,
						EndLine: int(child.EndPoint().Row) + 1,
						Content: child.Content(sourceCode),
						Scope:   scope,
					})
				}
			}
		}

	case "impl_item":
		// Rust impl Foo / impl Bar for Foo
		if typeNode := node.ChildByFieldName("type"); typeNode != nil {
			implType := typeNode.Content(sourceCode)
			nextScope = joinScope(scope, implType)
		}

	case "module":
		// Ruby module Foo
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextScope = joinScope(scope, name)
			contextData.Functions = append(contextData.Functions, CodeElement{
				Name:    name,
				Kind:    "Module",
				Line:    startLine,
				EndLine: endLine,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "namespace_declaration":
		// C# namespace Foo
		name := extractNodeFieldOrChild(node, "name", sourceCode)
		if name != "" {
			nextScope = joinScope(scope, name)
		}

	// ─── VARIABLES & DECLARATIONS ────────────────────────────────────────────
	case "assignment":
		// Python, Ruby, PHP
		if left := node.ChildByFieldName("left"); left != nil {
			varName := left.Content(sourceCode)
			if varName != "" && !strings.Contains(varName, ".") && !strings.Contains(varName, "[") {
				contextData.Variables = append(contextData.Variables, CodeElement{
					Name:    varName,
					Kind:    "Variable",
					Line:    startLine,
					EndLine: endLine,
					Content: node.Content(sourceCode),
					Scope:   scope,
				})
			}
		}

	case "lexical_declaration", "variable_declaration":
		// JavaScript, TypeScript
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() != "variable_declarator" {
				continue
			}
			nameNode := child.ChildByFieldName("name")
			valueNode := child.ChildByFieldName("value")
			if nameNode == nil {
				continue
			}
			valueContent := ""
			if valueNode != nil {
				valueContent = valueNode.Content(sourceCode)
			}
			switch nameNode.Type() {
			case "identifier":
				name := nameNode.Content(sourceCode)
				line := int(nameNode.StartPoint().Row) + 1
				endL := int(nameNode.EndPoint().Row) + 1
				if IsFunctionLike(valueNode) {
					childFunction := name
					childScope := joinScope(scope, name)
					contextData.Functions = append(contextData.Functions, CodeElement{
						Name:      name,
						Kind:      "Function",
						Line:      line,
						EndLine:   endL,
						Content:   valueContent,
						Scope:     scope,
						Signature: signatureOf(valueNode, sourceCode),
					})
					if valueNode != nil {
						extractLanguageData(valueNode, sourceCode, contextData, childScope, childFunction, lang)
					}
				} else {
					contextData.Variables = append(contextData.Variables, CodeElement{
						Name:    name,
						Kind:    "Variable",
						Line:    line,
						EndLine: endL,
						Content: valueContent,
						Scope:   scope,
					})
				}
			case "array_pattern", "object_pattern":
				identifiers := ExtractPatternIdentifiers(nameNode, sourceCode, valueContent)
				for j := range identifiers {
					identifiers[j].Scope = scope
					identifiers[j].EndLine = endLine
				}
				contextData.Variables = append(contextData.Variables, identifiers...)
			}
		}
		return

	case "var_declaration", "const_declaration":
		// Go var x / const y
		for i := 0; i < int(node.ChildCount()); i++ {
			spec := node.Child(i)
			if spec.Type() == "var_spec" || spec.Type() == "const_spec" {
				nameNode := spec.ChildByFieldName("name")
				if nameNode != nil {
					contextData.Variables = append(contextData.Variables, CodeElement{
						Name:    nameNode.Content(sourceCode),
						Kind:    "Variable",
						Line:    int(nameNode.StartPoint().Row) + 1,
						EndLine: int(spec.EndPoint().Row) + 1,
						Content: spec.Content(sourceCode),
						Scope:   scope,
					})
				}
			}
		}

	case "short_var_declaration":
		// Go x := 1
		if left := node.ChildByFieldName("left"); left != nil {
			for i := 0; i < int(left.ChildCount()); i++ {
				child := left.Child(i)
				if child.Type() == "identifier" {
					contextData.Variables = append(contextData.Variables, CodeElement{
						Name:    child.Content(sourceCode),
						Kind:    "Variable",
						Line:    startLine,
						EndLine: endLine,
						Content: node.Content(sourceCode),
						Scope:   scope,
					})
				}
			}
		}

	case "let_declaration", "const_item", "static_item":
		// Rust let / const / static
		nameNode := node.ChildByFieldName("pattern")
		if nameNode == nil {
			nameNode = node.ChildByFieldName("name")
		}
		if nameNode != nil {
			contextData.Variables = append(contextData.Variables, CodeElement{
				Name:    nameNode.Content(sourceCode),
				Kind:    "Variable",
				Line:    startLine,
				EndLine: endLine,
				Content: node.Content(sourceCode),
				Scope:   scope,
			})
		}

	case "field_declaration", "local_variable_declaration":
		// Java, C#
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "variable_declarator" {
				nameNode := child.ChildByFieldName("name")
				if nameNode != nil {
					contextData.Variables = append(contextData.Variables, CodeElement{
						Name:    nameNode.Content(sourceCode),
						Kind:    "Variable",
						Line:    startLine,
						EndLine: endLine,
						Content: child.Content(sourceCode),
						Scope:   scope,
					})
				}
			}
		}

	// ─── IMPORTS ─────────────────────────────────────────────────────────────
	case "import_statement", "import_from_statement", "import_declaration", "preproc_include", "using_directive", "use_declaration", "namespace_use_declaration":
		importText := strings.TrimSpace(node.Content(sourceCode))
		contextData.Imports = append(contextData.Imports, CodeElement{
			Name:    importText,
			Kind:    "Import",
			Line:    startLine,
			EndLine: endLine,
			Content: importText,
			Scope:   scope,
		})

	// ─── HTML ELEMENTS ───────────────────────────────────────────────────────
	case "element":
		if lang == "html" {
			if startTag := node.Child(0); startTag != nil && startTag.Type() == "start_tag" {
				if tagName := startTag.Child(1); tagName != nil {
					name := tagName.Content(sourceCode)
					contextData.Functions = append(contextData.Functions, CodeElement{
						Name:    name,
						Kind:    "Element",
						Line:    startLine,
						EndLine: endLine,
						Content: node.Content(sourceCode),
						Scope:   scope,
					})
				}
			}
		}

	// ─── CALLS ───────────────────────────────────────────────────────────────
	case "call_expression", "call", "method_invocation", "invocation_expression", "function_call_expression":
		if nextFunction != "" {
			callName := extractCallName(node, sourceCode)
			if callName != "" {
				recordCallOnFunction(contextData, nextFunction, callName)
			}
		}
	}

	// Recurse into children with updated scope and current function
	for i := 0; i < int(node.ChildCount()); i++ {
		extractLanguageData(node.Child(i), sourceCode, contextData, nextScope, nextFunction, lang)
	}
}

// extractFunctionName extracts function name across diverse grammars.
func extractFunctionName(node *sitter.Node, sourceCode []byte) string {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return nameNode.Content(sourceCode)
	}
	// In C/C++: declarator can be function_declarator
	if decl := node.ChildByFieldName("declarator"); decl != nil {
		return extractCDeclaratorName(decl, sourceCode)
	}
	return ""
}

// extractMethodName extracts method name across diverse grammars.
func extractMethodName(node *sitter.Node, sourceCode []byte) string {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return nameNode.Content(sourceCode)
	}
	return ""
}

// extractNodeFieldOrChild attempts to extract content by field name, or falls back to first identifier child.
func extractNodeFieldOrChild(node *sitter.Node, field string, sourceCode []byte) string {
	if child := node.ChildByFieldName(field); child != nil {
		return child.Content(sourceCode)
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c.Type() == "identifier" || c.Type() == "type_identifier" || c.Type() == "constant" {
			return c.Content(sourceCode)
		}
	}
	return ""
}

// extractGoReceiverType extracts the receiver type name for a Go method (e.g. "Server" or "*Server").
func extractGoReceiverType(rcvrNode *sitter.Node, sourceCode []byte) string {
	content := strings.TrimSpace(rcvrNode.Content(sourceCode))
	content = strings.TrimPrefix(content, "(")
	content = strings.TrimSuffix(content, ")")
	parts := strings.Fields(content)
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return ""
}

// extractCDeclaratorName extracts the identifier from C/C++ nested declarators.
func extractCDeclaratorName(node *sitter.Node, sourceCode []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case "identifier", "field_identifier":
		return node.Content(sourceCode)
	case "function_declarator", "pointer_declarator", "parenthesized_declarator":
		if d := node.ChildByFieldName("declarator"); d != nil {
			return extractCDeclaratorName(d, sourceCode)
		}
		if node.ChildCount() > 0 {
			return extractCDeclaratorName(node.Child(0), sourceCode)
		}
	case "qualified_identifier":
		if n := node.ChildByFieldName("name"); n != nil {
			return n.Content(sourceCode)
		}
	}
	return ""
}

// extractCallName extracts callee identifier for call expressions across languages.
func extractCallName(node *sitter.Node, sourceCode []byte) string {
	callee := node.ChildByFieldName("function")
	if callee == nil {
		callee = node.ChildByFieldName("name")
	}
	if callee == nil {
		callee = node.ChildByFieldName("method")
	}
	if callee == nil && node.ChildCount() > 0 {
		callee = node.Child(0)
	}
	if callee != nil {
		return strings.TrimSpace(callee.Content(sourceCode))
	}
	return ""
}

// recordCallOnFunction appends callName to the matching function's Calls list without duplicates.
func recordCallOnFunction(contextData *ASTContext, funcName, callName string) {
	for i := range contextData.Functions {
		if contextData.Functions[i].Name == funcName {
			for _, existing := range contextData.Functions[i].Calls {
				if existing == callName {
					return
				}
			}
			contextData.Functions[i].Calls = append(contextData.Functions[i].Calls, callName)
			return
		}
	}
}

// collectDiagnostics collects syntax errors and missing nodes across the tree.
func collectDiagnostics(node *sitter.Node, sourceCode []byte, diagnostics *[]Diagnostic) {
	if node == nil {
		return
	}
	if node.IsMissing() {
		*diagnostics = append(*diagnostics, Diagnostic{
			Message:  "missing " + node.Type(),
			Line:     int(node.StartPoint().Row) + 1,
			Column:   int(node.StartPoint().Column) + 1,
			Severity: "error",
		})
	} else if node.Type() == "ERROR" {
		content := node.Content(sourceCode)
		if len(content) > 40 {
			content = content[:40] + "..."
		}
		*diagnostics = append(*diagnostics, Diagnostic{
			Message:  "syntax error near " + content,
			Line:     int(node.StartPoint().Row) + 1,
			Column:   int(node.StartPoint().Column) + 1,
			Severity: "error",
		})
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		collectDiagnostics(node.Child(i), sourceCode, diagnostics)
	}
}
