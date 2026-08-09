// types defines the AST conflict payload and operation structures shared across the engine.
package engine

type ASTOperation struct {
	Action         string `toon:"a"`
	EnclosingBlock string `toon:"s"`
	LocalCode      string `toon:"l,omitempty"`
	RemoteCode     string `toon:"r,omitempty"`
}

type ConflictPayload struct {
	FilePath   string         `toon:"f"`
	Operations []ASTOperation `toon:"ops"`
}
