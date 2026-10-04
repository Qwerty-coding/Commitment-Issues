package promptcontext

import (
	"fmt"
	"sort"
	"strings"

	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

type PromptContextIR struct {
	RepositorySummary string               `json:"repositorySummary"`
	// RepositoryReadme is an excerpt of the repo-root README, loaded once per
	// repository and shared across all conflict files in that run. The field
	// name is the contract the frontend already consumes; do not rename it.
	RepositoryReadme string               `json:"repositoryReadme,omitempty"`
	Files            []string             `json:"files"`
	Functions        []parser.CodeElement `json:"functions"`
	// Variables holds conflict-scope variable declarations. They are the last
	// thing trimmed when applying the soft prompt budget (AI_TARGET_PROMPT_TOKENS).
	Variables []parser.CodeElement `json:"variables,omitempty"`
	Imports   []string             `json:"imports"`
	Context   string               `json:"context"`
	// EstimatedTokens is a cheap (bytes/4) approximation of the rendered
	// context size, used for soft prompt budgeting.
	EstimatedTokens int `json:"estimatedTokens"`
}

// DefaultTargetPromptTokens is the soft prompt-context budget used when no
// AI_TARGET_PROMPT_TOKENS override is supplied. It is deliberately generous:
// it only trims the auxiliary context, never the collision payload.
const DefaultTargetPromptTokens = 8000

// BuildPromptContext constructs the PromptContextIR for one conflicted file.
// repositoryReadme is the repo-root README excerpt (may be empty when README
// is absent or when the feature is disabled); it is included verbatim in the
// rendered context and stored on the IR for the frontend.
func BuildPromptContext(repositorySummary, repositoryReadme string, files []string, scope semantic.SemanticGraph, ourASTData, theirASTData parser.ASTContext) PromptContextIR {
	functionByKey := buildCodeElementIndex(ourASTData)
	for key, element := range buildCodeElementIndex(theirASTData) {
		if _, exists := functionByKey[key]; !exists {
			functionByKey[key] = element
		}
	}

	selectedFunctions := make([]parser.CodeElement, 0, len(scope.Nodes))
	selectedVariables := make([]parser.CodeElement, 0)
	seen := make(map[string]struct{}, len(scope.Nodes))
	for _, node := range scope.Nodes {
		isFunction := strings.EqualFold(node.Kind, "Function")
		isVariable := strings.EqualFold(node.Kind, "Variable")
		if !isFunction && !isVariable {
			continue
		}

		key := node.Identity
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		element, ok := functionByKey[key]
		if !ok {
			continue
		}
		if isFunction {
			selectedFunctions = append(selectedFunctions, element)
		} else {
			selectedVariables = append(selectedVariables, element)
		}
	}

	sort.Slice(selectedFunctions, func(i, j int) bool {
		if selectedFunctions[i].Line != selectedFunctions[j].Line {
			return selectedFunctions[i].Line < selectedFunctions[j].Line
		}
		if selectedFunctions[i].Name != selectedFunctions[j].Name {
			return selectedFunctions[i].Name < selectedFunctions[j].Name
		}
		return selectedFunctions[i].Kind < selectedFunctions[j].Kind
	})

	filesCopy := uniqueStrings(files)
	imports := mergeImports(ourASTData, theirASTData)
	language := ""
	if len(filesCopy) > 0 {
		language = parser.GetLanguageName(filesCopy[0])
	}
	contextText := renderPromptContext(repositorySummary, repositoryReadme, filesCopy, selectedFunctions, selectedVariables, imports, language)

	return PromptContextIR{
		RepositorySummary: repositorySummary,
		RepositoryReadme:  repositoryReadme,
		Files:             filesCopy,
		Functions:         selectedFunctions,
		Variables:         selectedVariables,
		Imports:           imports,
		Context:           contextText,
		EstimatedTokens:   EstimateTokens(contextText),
	}
}

// ApplyTargetBudget trims an assembled prompt context so its estimated size
// fits within targetTokens. Trimming follows a strict, documented order:
//
//	1. the README excerpt,
//	2. functions (from the tail, never the collision payload),
//	3. variables (from the tail).
//
// The collision payload is not part of PromptContextIR — it is supplied
// separately to the resolver — so it can never be trimmed here. A
// targetTokens <= 0 disables budgeting.
func ApplyTargetBudget(ir PromptContextIR, targetTokens int) PromptContextIR {
	if targetTokens <= 0 || ir.EstimatedTokens <= targetTokens {
		return ir
	}

	// 1. Drop the README excerpt first.
	if ir.RepositoryReadme != "" {
		ir = rerender(ir, "", ir.Functions, ir.Variables)
		if ir.EstimatedTokens <= targetTokens {
			return ir
		}
	}

	// 2. Trim functions from the tail.
	functions := append([]parser.CodeElement(nil), ir.Functions...)
	for len(functions) > 0 && ir.EstimatedTokens > targetTokens {
		functions = functions[:len(functions)-1]
		ir = rerender(ir, "", functions, ir.Variables)
	}

	// 3. Trim variables from the tail.
	variables := append([]parser.CodeElement(nil), ir.Variables...)
	for len(variables) > 0 && ir.EstimatedTokens > targetTokens {
		variables = variables[:len(variables)-1]
		ir = rerender(ir, "", functions, variables)
	}

	return ir
}

// rerender rebuilds the rendered context and token estimate after a trim.
func rerender(ir PromptContextIR, readme string, functions, variables []parser.CodeElement) PromptContextIR {
	language := ""
	if len(ir.Files) > 0 {
		language = parser.GetLanguageName(ir.Files[0])
	}
	ir.RepositoryReadme = readme
	ir.Functions = functions
	ir.Variables = variables
	ir.Context = renderPromptContext(ir.RepositorySummary, readme, ir.Files, functions, variables, ir.Imports, language)
	ir.EstimatedTokens = EstimateTokens(ir.Context)
	return ir
}

// mergeImports combines import statements from both sides of a conflict,
// deduplicated and deterministically sorted.
func mergeImports(ourASTData, theirASTData parser.ASTContext) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0, len(ourASTData.Imports)+len(theirASTData.Imports))
	for _, ctx := range []parser.ASTContext{ourASTData, theirASTData} {
		for _, imp := range ctx.Imports {
			text := strings.TrimSpace(imp.Content)
			if text == "" {
				continue
			}
			if _, exists := seen[text]; exists {
				continue
			}
			seen[text] = struct{}{}
			merged = append(merged, text)
		}
	}
	sort.Strings(merged)
	return merged
}

func EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return (len(trimmed) + 3) / 4
}

func buildCodeElementIndex(ctx parser.ASTContext) map[string]parser.CodeElement {
	index := make(map[string]parser.CodeElement, len(ctx.Functions)+len(ctx.Variables))
	for _, fn := range ctx.Functions {
		index[semantic.SymbolIdentity(fn)] = fn
	}
	for _, variable := range ctx.Variables {
		index[semantic.SymbolIdentity(variable)] = variable
	}
	return index
}

// renderPromptContext builds the text payload that is sent to the AI model.
// When repositoryReadme is non-empty a "Repository README (excerpt):" section
// is rendered before "Files in scope" so the model has project context.
func renderPromptContext(repositorySummary, repositoryReadme string, files []string, functions, variables []parser.CodeElement, imports []string, language string) string {
	var builder strings.Builder

	builder.WriteString("Repository summary:\n")
	builder.WriteString(repositorySummary)
	builder.WriteString("\n\n")

	if repositoryReadme != "" {
		builder.WriteString("Repository README (excerpt):\n")
		builder.WriteString(repositoryReadme)
		if !strings.HasSuffix(repositoryReadme, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	builder.WriteString("Files in scope:\n")
	if len(files) == 0 {
		builder.WriteString("- (none)\n")
	} else {
		for _, fileName := range files {
			builder.WriteString("- ")
			builder.WriteString(fileName)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n")

	builder.WriteString("Relevant functions:\n")
	if len(functions) == 0 {
		builder.WriteString("- (none)\n")
	} else {
		// The fence label must match the actual language of the conflicted
		// file; a wrong label (e.g. always javascript) mis-anchors the model.
		fence := "```"
		if language != "" {
			fence = "```" + language
		}
		for _, fn := range functions {
			builder.WriteString(fmt.Sprintf("### %s (%s, line %d)\n", fn.Name, fn.Kind, fn.Line))
			builder.WriteString(fence)
			builder.WriteString("\n")
			builder.WriteString(strings.TrimSpace(fn.Content))
			builder.WriteString("\n```\n\n")
		}
	}

	if len(variables) > 0 {
		fence := "```"
		if language != "" {
			fence = "```" + language
		}
		builder.WriteString("Relevant variables:\n")
		for _, v := range variables {
			builder.WriteString(fmt.Sprintf("### %s (%s, line %d)\n", v.Name, v.Kind, v.Line))
			builder.WriteString(fence)
			builder.WriteString("\n")
			builder.WriteString(strings.TrimSpace(v.Content))
			builder.WriteString("\n```\n\n")
		}
	}

	builder.WriteString("Imports:\n")
	if len(imports) == 0 {
		builder.WriteString("- (none)\n")
	} else {
		for _, imp := range imports {
			builder.WriteString("- ")
			builder.WriteString(imp)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n")
	builder.WriteString("Use only the scoped functions above and the collision payload provided next to resolve the merge conflict.")

	return builder.String()
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
