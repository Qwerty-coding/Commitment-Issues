package semantic

import (
	"context"
	"fmt"
	"sort"
	"strings"

	parser "CommitIssues/internal/parser"
)

type DiffItem struct {
	Type         string `json:"type"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Line         int    `json:"line"`
	BaseContent  string `json:"base_content,omitempty"`
	OurContent   string `json:"our_content,omitempty"`
	TheirContent string `json:"their_content,omitempty"`
	// Identity is the precise symbol identity (file + scope + kind + name +
	// signature) when available, falling back to kind:name.
	Identity string `json:"identity,omitempty"`
}

type SmartDiffResult struct {
	Collisions   []DiffItem `json:"collisions"`
	OurChanges   []DiffItem `json:"our_changes"`
	TheirChanges []DiffItem `json:"their_changes"`
}

type SemanticNode struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Label string   `json:"label"`
	Line  int      `json:"line,omitempty"`
	Calls []string `json:"calls,omitempty"`
	// Identity is the precise symbol key used to keep distinct symbols
	// (overloads, methods in different classes, scoped symbols) separate.
	Identity string `json:"identity,omitempty"`
}

type SemanticEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

type SemanticGraph struct {
	Nodes []SemanticNode `json:"nodes"`
	Edges []SemanticEdge `json:"edges"`
}

type ConflictScope = SemanticGraph

// SymbolIdentity builds a deterministic identity for a code element from its
// file, enclosing scope, kind, name and signature. When none of the richer
// fields are present it degrades to the legacy "kind:name" key.
func SymbolIdentity(el parser.CodeElement) string {
	if el.File == "" && el.Scope == "" && el.Signature == "" {
		return el.Kind + ":" + el.Name
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", el.File, el.Scope, el.Kind, el.Name, el.Signature)
}

// DiffKey returns the key used to match a diff item against graph nodes.
func DiffKey(item DiffItem) string {
	if item.Identity != "" {
		return item.Identity
	}
	return strings.ToLower(item.Kind) + ":" + item.Name
}

func BuildSignatureMap(ctx parser.ASTContext) map[string]parser.CodeElement {
	m := make(map[string]parser.CodeElement, len(ctx.Functions)+len(ctx.Variables))
	for _, fn := range ctx.Functions {
		m[SymbolIdentity(fn)] = fn
	}
	for _, v := range ctx.Variables {
		m[SymbolIdentity(v)] = v
	}
	return m
}

func SideStatus(baseEl, sideEl parser.CodeElement, inBase, inSide bool) string {
	switch {
	case !inBase && inSide:
		return "ADDED"
	case inBase && !inSide:
		return "DELETED"
	case inBase && inSide && baseEl.Content != sideEl.Content:
		return "UPDATED"
	default:
		return ""
	}
}

func BuildDiffItem(status string, baseEl, sideEl parser.CodeElement, isOurs bool) DiffItem {
	item := DiffItem{Type: status}

	if status == "DELETED" {
		item.Kind, item.Name, item.Line = baseEl.Kind, baseEl.Name, baseEl.Line
		item.BaseContent = baseEl.Content
		item.Identity = SymbolIdentity(baseEl)
		return item
	}

	item.Kind, item.Name, item.Line = sideEl.Kind, sideEl.Name, sideEl.Line
	item.Identity = SymbolIdentity(sideEl)
	if status == "UPDATED" {
		item.BaseContent = baseEl.Content
	}
	if isOurs {
		item.OurContent = sideEl.Content
	} else {
		item.TheirContent = sideEl.Content
	}
	return item
}

// GenerateSmartDiffContext is GenerateSmartDiff with cancellation support so
// semantic analysis participates in context propagation.
func GenerateSmartDiffContext(ctx context.Context, base, ours, theirs parser.ASTContext) (SmartDiffResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SmartDiffResult{}, err
	}
	result := GenerateSmartDiff(base, ours, theirs)
	if err := ctx.Err(); err != nil {
		return SmartDiffResult{}, err
	}
	return result, nil
}

func GenerateSmartDiff(base, ours, theirs parser.ASTContext) SmartDiffResult {
	baseMap := BuildSignatureMap(base)
	oursMap := BuildSignatureMap(ours)
	theirsMap := BuildSignatureMap(theirs)

	signatures := make(map[string]struct{}, len(oursMap)+len(theirsMap))
	for sig := range oursMap {
		signatures[sig] = struct{}{}
	}
	for sig := range theirsMap {
		signatures[sig] = struct{}{}
	}

	result := SmartDiffResult{
		Collisions:   []DiffItem{},
		OurChanges:   []DiffItem{},
		TheirChanges: []DiffItem{},
	}
	isChangeSide := func(s string) bool { return s == "ADDED" || s == "UPDATED" }

	for sig := range signatures {
		baseEl, inBase := baseMap[sig]
		ourEl, inOurs := oursMap[sig]
		theirEl, inTheirs := theirsMap[sig]

		ourStat := SideStatus(baseEl, ourEl, inBase, inOurs)
		theirStat := SideStatus(baseEl, theirEl, inBase, inTheirs)

		if isChangeSide(ourStat) && isChangeSide(theirStat) && ourEl.Content != theirEl.Content {
			result.Collisions = append(result.Collisions, DiffItem{
				Type: "COLLISION", Kind: ourEl.Kind, Name: ourEl.Name, Line: ourEl.Line,
				BaseContent: baseEl.Content, OurContent: ourEl.Content, TheirContent: theirEl.Content,
				Identity: sig,
			})
			continue
		}

		if ourStat != "" {
			result.OurChanges = append(result.OurChanges, BuildDiffItem(ourStat, baseEl, ourEl, true))
		}
		if theirStat != "" {
			result.TheirChanges = append(result.TheirChanges, BuildDiffItem(theirStat, baseEl, theirEl, false))
		}
	}

	statusRank := map[string]int{"ADDED": 0, "UPDATED": 1, "DELETED": 2}
	sortChanges := func(list []DiffItem) {
		sort.Slice(list, func(i, j int) bool {
			if statusRank[list[i].Type] != statusRank[list[j].Type] {
				return statusRank[list[i].Type] < statusRank[list[j].Type]
			}
			return list[i].Line < list[j].Line
		})
	}
	sortChanges(result.OurChanges)
	sortChanges(result.TheirChanges)
	sort.Slice(result.Collisions, func(i, j int) bool { return result.Collisions[i].Name < result.Collisions[j].Name })

	return result
}

func BuildSemanticGraph(ctx parser.ASTContext) SemanticGraph {
	result := SemanticGraph{Nodes: []SemanticNode{}, Edges: []SemanticEdge{}}
	nodeByKey := make(map[string]SemanticNode)
	funcByName := make(map[string]SemanticNode)

	for _, fn := range ctx.Functions {
		node := SemanticNode{
			ID:       semanticNodeIDFor(fn),
			Name:     fn.Name,
			Kind:     fn.Kind,
			Label:    fn.Name,
			Line:     fn.Line,
			Calls:    append([]string(nil), fn.Calls...),
			Identity: SymbolIdentity(fn),
		}
		result.Nodes = append(result.Nodes, node)
		nodeByKey[node.Identity] = node
		if node.Kind == "Function" {
			nameKey := strings.ToLower(node.Name)
			if existing, ok := funcByName[nameKey]; !ok || node.ID < existing.ID {
				funcByName[nameKey] = node
			}
		}
	}

	for _, v := range ctx.Variables {
		node := SemanticNode{
			ID:       semanticNodeIDFor(v),
			Name:     v.Name,
			Kind:     v.Kind,
			Label:    v.Name,
			Line:     v.Line,
			Identity: SymbolIdentity(v),
		}
		result.Nodes = append(result.Nodes, node)
		nodeByKey[node.Identity] = node
	}

	edgeSeen := make(map[string]struct{})
	for _, fn := range ctx.Functions {
		if fn.Kind != "Function" {
			continue
		}
		sourceID := semanticNodeIDFor(fn)
		for _, callName := range fn.Calls {
			targetNode, ok := funcByName[strings.ToLower(callName)]
			if !ok {
				continue
			}
			edgeID := fmt.Sprintf("%s__CALLS__%s", sourceID, targetNode.ID)
			if _, exists := edgeSeen[edgeID]; exists {
				continue
			}
			edgeSeen[edgeID] = struct{}{}
			result.Edges = append(result.Edges, SemanticEdge{
				ID:     edgeID,
				Source: sourceID,
				Target: targetNode.ID,
				Type:   "CALLS",
			})
		}
	}

	return result
}

func MergeSemanticGraphs(graphs ...SemanticGraph) SemanticGraph {
	merged := SemanticGraph{Nodes: []SemanticNode{}, Edges: []SemanticEdge{}}
	if len(graphs) == 0 {
		return merged
	}

	seenNodes := make(map[string]struct{})
	for _, graph := range graphs {
		for _, node := range graph.Nodes {
			if _, exists := seenNodes[node.ID]; exists {
				continue
			}
			seenNodes[node.ID] = struct{}{}
			merged.Nodes = append(merged.Nodes, node)
		}
	}

	seenEdges := make(map[string]struct{})
	for _, graph := range graphs {
		for _, edge := range graph.Edges {
			if _, exists := seenEdges[edge.ID]; exists {
				continue
			}
			seenEdges[edge.ID] = struct{}{}
			merged.Edges = append(merged.Edges, edge)
		}
	}

	return merged
}

func ComputeConflictScope(graph SemanticGraph, collisions []DiffItem) ConflictScope {
	if len(graph.Nodes) == 0 {
		return ConflictScope{Nodes: []SemanticNode{}, Edges: []SemanticEdge{}}
	}

	nodeByID := make(map[string]SemanticNode, len(graph.Nodes))
	nodeByKey := make(map[string]SemanticNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
		if node.Identity != "" {
			nodeByKey[node.Identity] = node
		}
		fallbackKey := semanticNodeKey(node.Kind, node.Name)
		if _, exists := nodeByKey[fallbackKey]; !exists {
			nodeByKey[fallbackKey] = node
		}
	}

	outgoing := make(map[string][]SemanticEdge)
	incoming := make(map[string][]SemanticEdge)
	for _, edge := range graph.Edges {
		if edge.Type != "CALLS" {
			continue
		}
		outgoing[edge.Source] = append(outgoing[edge.Source], edge)
		incoming[edge.Target] = append(incoming[edge.Target], edge)
	}

	queue := make([]string, 0)
	visited := make(map[string]struct{})
	scopeNodes := make(map[string]SemanticNode)
	scopeEdges := make(map[string]SemanticEdge)

	for _, collision := range collisions {
		if !strings.EqualFold(collision.Kind, "Function") {
			continue
		}
		root, ok := nodeByKey[DiffKey(collision)]
		if !ok {
			root, ok = nodeByKey[semanticNodeKey("Function", collision.Name)]
		}
		if ok {
			if _, seen := visited[root.ID]; !seen {
				visited[root.ID] = struct{}{}
				queue = append(queue, root.ID)
				scopeNodes[root.ID] = root
			}
		}
	}

	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]

		for _, edge := range outgoing[currentID] {
			scopeEdges[edge.ID] = edge
			if _, seen := visited[edge.Target]; !seen {
				if target, ok := nodeByID[edge.Target]; ok {
					visited[edge.Target] = struct{}{}
					scopeNodes[target.ID] = target
					queue = append(queue, target.ID)
				}
			}
		}

		for _, edge := range incoming[currentID] {
			scopeEdges[edge.ID] = edge
			if _, seen := visited[edge.Source]; !seen {
				if source, ok := nodeByID[edge.Source]; ok {
					visited[edge.Source] = struct{}{}
					scopeNodes[source.ID] = source
					queue = append(queue, source.ID)
				}
			}
		}
	}

	nodes := make([]SemanticNode, 0, len(scopeNodes))
	for _, node := range scopeNodes {
		nodes = append(nodes, node)
	}
	edges := make([]SemanticEdge, 0, len(scopeEdges))
	for _, edge := range scopeEdges {
		edges = append(edges, edge)
	}

	// Deterministic ordering regardless of map iteration order.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	return ConflictScope{Nodes: nodes, Edges: edges}
}

func semanticNodeKey(kind, name string) string {
	return strings.ToLower(kind) + ":" + name
}

func semanticNodeID(kind, name string) string {
	return strings.ToLower(kind) + "__" + strings.NewReplacer("/", "_", "\\", "_", ":", "_", ".", "_", " ", "_").Replace(name)
}

// semanticNodeIDFor builds a graph node ID from an element's full identity so
// that duplicate names in different files/scopes/signatures stay distinct,
// while degrading to the legacy ID when no richer metadata is present.
func semanticNodeIDFor(el parser.CodeElement) string {
	if el.File == "" && el.Scope == "" && el.Signature == "" {
		return semanticNodeID(el.Kind, el.Name)
	}
	composite := strings.Join([]string{el.File, el.Scope, el.Name, el.Signature}, "__")
	return semanticNodeID(el.Kind, composite)
}
