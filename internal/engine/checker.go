// checker contains lightweight deterministic heuristics for simple merge decisions.
package engine

import (
	"crypto/sha256"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// CheckResult describes the outcome of a simple deterministic merge heuristic.
type CheckResult struct {
	Resolved   bool
	MergedCode string
	Reason     string
}

// EvaluateBasicChecker applies a few lightweight heuristics for obvious node-level merges.
func EvaluateBasicChecker(localNode, remoteNode *sitter.Node, localSrc, remoteSrc []byte) CheckResult {
	if localNode == nil || remoteNode == nil {
		return CheckResult{Resolved: false}
	}

	if localNode.Type() == "function_item" && remoteNode.Type() == "function_item" {
		if hashContent(localNode.Content(localSrc)) == hashContent(remoteNode.Content(remoteSrc)) {
			return CheckResult{Resolved: true, MergedCode: localNode.Content(localSrc), Reason: "Function contents are identical"}
		}
	}

	if (localNode.Type() == "identifier" || localNode.Type() == "type_identifier") &&
		(remoteNode.Type() == "identifier" || remoteNode.Type() == "type_identifier") {
		return CheckResult{Resolved: true, MergedCode: localNode.Content(localSrc), Reason: "Identifier or type node matched"}
	}

	if localNode.Type() == "binary_expression" && remoteNode.Type() == "binary_expression" &&
		localNode.NamedChildCount() == remoteNode.NamedChildCount() {
		return CheckResult{Resolved: true, MergedCode: remoteNode.Content(remoteSrc), Reason: "Binary expression structure matched"}
	}

	return CheckResult{Resolved: false}
}

func hashContent(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum(nil))
}
