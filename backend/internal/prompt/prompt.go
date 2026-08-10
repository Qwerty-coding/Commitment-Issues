package prompt

import (
	"fmt"
	"strings"

	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

type AIRequestPayload struct {
	FileName     string                   `json:"file_name"`
	BaseCode     string                   `json:"base_code"`
	OurCode      string                   `json:"our_code"`
	TheirCode    string                   `json:"their_code"`
	OurASTData   parser.ASTContext        `json:"our_ast_data"`
	TheirASTData parser.ASTContext        `json:"their_ast_data"`
	SmartDiff    semantic.SmartDiffResult `json:"smart_diff"`
}

func MarshalAIRequestPayload(payload AIRequestPayload) ([]byte, error) {
	return []byte(marshalTOON(payload)), nil
}

func marshalTOON(payload AIRequestPayload) string {
	var builder strings.Builder

	builder.WriteString("file_name: ")
	builder.WriteString(payload.FileName)
	builder.WriteString("\n\n")

	writeBlock(&builder, "base_code", payload.BaseCode)
	writeBlock(&builder, "our_code", payload.OurCode)
	writeBlock(&builder, "their_code", payload.TheirCode)

	builder.WriteString("our_ast_data:\n")
	writeASTContext(&builder, payload.OurASTData)
	builder.WriteString("their_ast_data:\n")
	writeASTContext(&builder, payload.TheirASTData)

	builder.WriteString("smart_diff:\n")
	writeDiffItems(&builder, "collisions", payload.SmartDiff.Collisions)
	writeDiffItems(&builder, "our_changes", payload.SmartDiff.OurChanges)
	writeDiffItems(&builder, "their_changes", payload.SmartDiff.TheirChanges)

	return builder.String()
}

func writeBlock(builder *strings.Builder, key, value string) {
	builder.WriteString(key)
	builder.WriteString(": |-")
	for _, line := range strings.Split(strings.TrimRight(value, "\n"), "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
}

func writeASTContext(builder *strings.Builder, ctx parser.ASTContext) {
	builder.WriteString("  functions:\n")
	for _, fn := range ctx.Functions {
		builder.WriteString("    - name: ")
		builder.WriteString(fn.Name)
		builder.WriteString("\n")
		builder.WriteString("      kind: ")
		builder.WriteString(fn.Kind)
		builder.WriteString("\n")
		builder.WriteString("      line: ")
		builder.WriteString(fmt.Sprintf("%d\n", fn.Line))
		builder.WriteString("      content: |-\n")
		for _, line := range strings.Split(strings.TrimRight(fn.Content, "\n"), "\n") {
			builder.WriteString("        ")
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("  variables:\n")
	for _, variable := range ctx.Variables {
		builder.WriteString("    - name: ")
		builder.WriteString(variable.Name)
		builder.WriteString("\n")
		builder.WriteString("      kind: ")
		builder.WriteString(variable.Kind)
		builder.WriteString("\n")
		builder.WriteString("      line: ")
		builder.WriteString(fmt.Sprintf("%d\n", variable.Line))
		builder.WriteString("      content: |-\n")
		for _, line := range strings.Split(strings.TrimRight(variable.Content, "\n"), "\n") {
			builder.WriteString("        ")
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
}

func writeDiffItems(builder *strings.Builder, key string, items []semantic.DiffItem) {
	builder.WriteString("  ")
	builder.WriteString(key)
	builder.WriteString(":\n")
	if len(items) == 0 {
		builder.WriteString("    - []\n")
		return
	}

	for _, item := range items {
		builder.WriteString("    - kind: ")
		builder.WriteString(item.Kind)
		builder.WriteString("\n")
		builder.WriteString("      name: ")
		builder.WriteString(item.Name)
		builder.WriteString("\n")
		builder.WriteString("      line: ")
		builder.WriteString(fmt.Sprintf("%d\n", item.Line))
		builder.WriteString("      type: ")
		builder.WriteString(item.Type)
		builder.WriteString("\n")
		writeBlock(builder, "      base_content", item.BaseContent)
		writeBlock(builder, "      our_content", item.OurContent)
		writeBlock(builder, "      their_content", item.TheirContent)
	}
}
