package prompt

import (
	"encoding/json"

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
	return json.MarshalIndent(payload, "", "  ")
}
