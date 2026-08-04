package engine

import (
	"crypto/sha256"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

type CheckResult struct {
	Resolved   bool
	MergedCode string
	Reason     string
}

// EvaluateBasicChecker runs deterministic heuristics on conflicting AST nodes.
func EvaluateBasicChecker(localNode, remoteNode *sitter.Node, localSrc, remoteSrc []byte) CheckResult {
	if localNode == nil || remoteNode == nil {
		return CheckResult{Resolved: false}
	}

	// 1. Check Function Reordering / Identical Content
	if localNode.Type() == "function_item" && remoteNode.Type() == "function_item" {
		localHash := hashContent(localNode.Content(localSrc))
		remoteHash := hashContent(remoteNode.Content(remoteSrc))
		if localHash == remoteHash {
			return CheckResult{
				Resolved:   true,
				MergedCode: localNode.Content(localSrc),
				Reason:     "Function reordering detected; AST contents are identical",
			}
		}
	}

	// 2. Variable Identifier / Type Rename Check
	if (localNode.Type() == "identifier" || localNode.Type() == "type_identifier") &&
		(remoteNode.Type() == "identifier" || remoteNode.Type() == "type_identifier") {
		return CheckResult{
			Resolved:   true,
			MergedCode: localNode.Content(localSrc),
			Reason:     fmt.Sprintf("Auto-resolved identifier/type migration: %s", localNode.Content(localSrc)),
		}
	}

	// 3. Operator Swap Check
	if localNode.Type() == "binary_expression" && remoteNode.Type() == "binary_expression" {
		if localNode.NamedChildCount() == remoteNode.NamedChildCount() {
			return CheckResult{
				Resolved:   true,
				MergedCode: remoteNode.Content(remoteSrc),
				Reason:     "Operator change auto-resolved via remote fast-forward",
			}
		}
	}

	return CheckResult{Resolved: false}
}

func hashContent(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum(nil))
}
