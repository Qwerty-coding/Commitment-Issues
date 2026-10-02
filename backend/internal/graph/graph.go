package graph

import (
	"fmt"
	"sort"
	"strings"

	semantic "CommitIssues/internal/semantic"
)

// ─── Cytoscape JSON structs (compound nodes) ──────────────────────────────

type CyNodeData struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	Kind      string `json:"kind"`
	Parent    string `json:"parent,omitempty"`
	Line      int    `json:"line,omitempty"`
	BaseCode  string `json:"base_code,omitempty"`
	OurCode   string `json:"our_code,omitempty"`
	TheirCode string `json:"their_code,omitempty"`
}

type CyNode struct {
	Data CyNodeData `json:"data"`
}

type CyElements struct {
	Nodes []CyNode `json:"nodes"`
	Edges []CyEdge `json:"edges"`
}

type CyGraph struct {
	Elements CyElements `json:"elements"`
}

type CyEdgeData struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

type CyEdge struct {
	Data CyEdgeData `json:"data"`
}

type GraphDTO struct {
	Nodes []CyNode `json:"nodes"`
	Edges []CyEdge `json:"edges"`
}

// BuildCyGraph builds a deterministic, duplicate-free Cytoscape graph for a
// single conflicted file. Node and edge order is stable regardless of map
// iteration order, so repeated scans produce identical output.
func BuildCyGraph(fileName string, diff semantic.SmartDiffResult, semanticGraphs ...semantic.SemanticGraph) CyGraph {
	nodes := []CyNode{}
	edges := []CyEdge{}
	nodeIDByKey := make(map[string]string)
	seenNodeIDs := make(map[string]struct{})

	rootID := "file__" + sanitizeID(fileName)
	appendNode := func(node CyNode) {
		if _, exists := seenNodeIDs[node.Data.ID]; exists {
			return
		}
		seenNodeIDs[node.Data.ID] = struct{}{}
		nodes = append(nodes, node)
	}

	appendNode(CyNode{Data: CyNodeData{
		ID:     rootID,
		Label:  fileName,
		Status: "file",
		Kind:   "file",
	}})

	type elemMeta struct {
		kind      string
		name      string
		line      int
		status    string
		baseCode  string
		ourCode   string
		theirCode string
	}
	statusMap := map[string]elemMeta{}

	for _, item := range diff.Collisions {
		statusMap[diffKey(item)] = elemMeta{
			kind: item.Kind, name: item.Name, line: item.Line, status: "collision",
			baseCode: item.BaseContent, ourCode: item.OurContent, theirCode: item.TheirContent,
		}
	}
	for _, item := range diff.OurChanges {
		k := diffKey(item)
		if _, exists := statusMap[k]; !exists {
			statusMap[k] = elemMeta{
				kind: item.Kind, name: item.Name, line: item.Line, status: strings.ToLower(item.Type),
				baseCode: item.BaseContent, ourCode: item.OurContent, theirCode: item.TheirContent,
			}
		}
	}
	for _, item := range diff.TheirChanges {
		k := diffKey(item)
		if _, exists := statusMap[k]; !exists {
			statusMap[k] = elemMeta{
				kind: item.Kind, name: item.Name, line: item.Line, status: strings.ToLower(item.Type),
				baseCode: item.BaseContent, ourCode: item.OurContent, theirCode: item.TheirContent,
			}
		}
	}

	keys := make([]string, 0, len(statusMap))
	for key := range statusMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		meta := statusMap[key]
		nodeID := sanitizeID(rootID + "__" + key)
		nodeIDByKey[key] = nodeID
		appendNode(CyNode{Data: CyNodeData{
			ID:        nodeID,
			Parent:    rootID,
			Label:     fmt.Sprintf("%s: %s (L%d)", meta.kind, meta.name, meta.line),
			Status:    meta.status,
			Kind:      meta.kind,
			Line:      meta.line,
			BaseCode:  meta.baseCode,
			OurCode:   meta.ourCode,
			TheirCode: meta.theirCode,
		}})
	}

	if len(semanticGraphs) > 0 {
		semanticToCyID := make(map[string]string)
		for _, node := range semanticGraphs[0].Nodes {
			key := node.Identity
			if key == "" {
				key = strings.ToLower(node.Kind) + ":" + node.Name
			}
			if cyID, ok := nodeIDByKey[key]; ok {
				semanticToCyID[node.ID] = cyID
			}
		}

		seenEdgeIDs := make(map[string]struct{})
		for _, edge := range semanticGraphs[0].Edges {
			if edge.Type != "CALLS" {
				continue
			}
			sourceNodeID, ok := semanticToCyID[edge.Source]
			if !ok {
				continue
			}
			targetNodeID, ok := semanticToCyID[edge.Target]
			if !ok {
				continue
			}
			edgeID := sanitizeID(edge.ID)
			if _, exists := seenEdgeIDs[edgeID]; exists {
				continue
			}
			seenEdgeIDs[edgeID] = struct{}{}
			edges = append(edges, CyEdge{Data: CyEdgeData{
				ID:     edgeID,
				Source: sourceNodeID,
				Target: targetNodeID,
				Type:   edge.Type,
			}})
		}
	}

	sort.Slice(edges, func(i, j int) bool { return edges[i].Data.ID < edges[j].Data.ID })

	return CyGraph{Elements: CyElements{Nodes: nodes, Edges: edges}}
}

// MergeGraphs merges multiple per-file graphs into one deterministic graph,
// deduplicating nodes and edges by ID and sorting the output.
func MergeGraphs(graphs []CyGraph) CyGraph {
	merged := CyGraph{Elements: CyElements{Nodes: []CyNode{}, Edges: []CyEdge{}}}

	seenNodes := make(map[string]struct{})
	seenEdges := make(map[string]struct{})
	for _, g := range graphs {
		for _, node := range g.Elements.Nodes {
			if _, exists := seenNodes[node.Data.ID]; exists {
				continue
			}
			seenNodes[node.Data.ID] = struct{}{}
			merged.Elements.Nodes = append(merged.Elements.Nodes, node)
		}
		for _, edge := range g.Elements.Edges {
			if _, exists := seenEdges[edge.Data.ID]; exists {
				continue
			}
			seenEdges[edge.Data.ID] = struct{}{}
			merged.Elements.Edges = append(merged.Elements.Edges, edge)
		}
	}

	sort.Slice(merged.Elements.Nodes, func(i, j int) bool {
		return merged.Elements.Nodes[i].Data.ID < merged.Elements.Nodes[j].Data.ID
	})
	sort.Slice(merged.Elements.Edges, func(i, j int) bool {
		return merged.Elements.Edges[i].Data.ID < merged.Elements.Edges[j].Data.ID
	})
	return merged
}

func diffKey(item semantic.DiffItem) string {
	if item.Identity != "" {
		return item.Identity
	}
	return strings.ToLower(item.Kind) + ":" + item.Name
}

func sanitizeID(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", ".", "_", " ", "_")
	return r.Replace(s)
}

// ToDTO converts a Cytoscape graph into the flat DTO shape the API returns.
func (g CyGraph) ToDTO() GraphDTO {
	return GraphDTO{
		Nodes: g.Elements.Nodes,
		Edges: g.Elements.Edges,
	}
}
