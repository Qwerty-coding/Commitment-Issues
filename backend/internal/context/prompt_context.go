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
	Files             []string             `json:"files"`
	Functions         []parser.CodeElement `json:"functions"`
	Imports           []string             `json:"imports"`
	Context           string               `json:"context"`
	EstimatedTokens   int                  `json:"estimatedTokens"`
}

func BuildPromptContext(repositorySummary string, files []string, scope semantic.SemanticGraph, ourASTData, theirASTData parser.ASTContext) PromptContextIR {
	functionByKey := buildCodeElementIndex(ourASTData)
	for key, element := range buildCodeElementIndex(theirASTData) {
		if _, exists := functionByKey[key]; !exists {
			functionByKey[key] = element
		}
	}

	selectedFunctions := make([]parser.CodeElement, 0, len(scope.Nodes))
	seen := make(map[string]struct{}, len(scope.Nodes))
	for _, node := range scope.Nodes {
		if !strings.EqualFold(node.Kind, "Function") {
			continue
		}

		key := node.Identity
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		if element, ok := functionByKey[key]; ok {
			selectedFunctions = append(selectedFunctions, element)
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
	contextText := renderPromptContext(repositorySummary, filesCopy, selectedFunctions)

	return PromptContextIR{
		RepositorySummary: repositorySummary,
		Files:             filesCopy,
		Functions:         selectedFunctions,
		Imports:           []string{},
		Context:           contextText,
		EstimatedTokens:   EstimateTokens(contextText),
	}
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

func renderPromptContext(repositorySummary string, files []string, functions []parser.CodeElement) string {
	var builder strings.Builder

	builder.WriteString("Repository summary:\n")
	builder.WriteString(repositorySummary)
	builder.WriteString("\n\n")

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
		for _, fn := range functions {
			builder.WriteString(fmt.Sprintf("### %s (%s, line %d)\n", fn.Name, fn.Kind, fn.Line))
			builder.WriteString("```javascript\n")
			builder.WriteString(strings.TrimSpace(fn.Content))
			builder.WriteString("\n```\n\n")
		}
	}

	builder.WriteString("Imports:\n- (none)\n\n")
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
