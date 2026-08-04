package engine

// ASTOperation captures structural edits mapped to their parent function.
// YAML tags are strictly used here to minimize LLM token consumption.
type ASTOperation struct {
	Action         string `yaml:"action"`          // UPDATE, DELETE, INSERT, MOVE
	EnclosingBlock string `yaml:"enclosing_scope"` // e.g., "fn process_data()"
	LocalCode      string `yaml:"local_code,omitempty"`
	RemoteCode     string `yaml:"remote_code,omitempty"`
}

// ConflictPayload represents the reduced-token payload passed to Gemini.
type ConflictPayload struct {
	FilePath   string         `yaml:"file"`
	Operations []ASTOperation `yaml:"operations"`
}
