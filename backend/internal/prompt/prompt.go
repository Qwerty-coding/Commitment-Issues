package prompt

import (
	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
	"CommitIssues/internal/toon"
)

// AIRequestPayload is everything the resolver receives for one collision.
// It is encoded in TOON (token-oriented notation) before being sent to a
// provider; the HTTP API, cache and eval goldens keep using JSON.
type AIRequestPayload struct {
	FileName     string                   `json:"file_name"`
	BaseCode     string                   `json:"base_code"`
	OurCode      string                   `json:"our_code"`
	TheirCode    string                   `json:"their_code"`
	OurASTData   parser.ASTContext        `json:"our_ast_data"`
	TheirASTData parser.ASTContext        `json:"their_ast_data"`
	SmartDiff    semantic.SmartDiffResult `json:"smart_diff"`
}

// MarshalAIRequestPayload encodes the payload with the spec TOON encoder.
// Deterministic by construction: struct field order follows declaration order
// and uniform arrays (the SmartDiff lists) use the compact tabular form.
func MarshalAIRequestPayload(payload AIRequestPayload) ([]byte, error) {
	encoded, err := toon.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return []byte(encoded), nil
}