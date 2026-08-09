package graph

import (
	"fmt"
	"strings"
	"sync"

	semantic "CommitIssues/internal/semantic"
)

// ─── Cytoscape JSON structs (UPDATED FOR COMPOUND NODES) ──────────────

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

func BuildCyGraph(fileName string, diff semantic.SmartDiffResult, semanticGraphs ...semantic.SemanticGraph) CyGraph {
	nodes := []CyNode{}
	edges := []CyEdge{}
	nodeIDByKey := make(map[string]string)

	rootID := "file__" + sanitizeID(fileName)
	nodes = append(nodes, CyNode{Data: CyNodeData{
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
		k := item.Kind + ":" + item.Name
		statusMap[k] = elemMeta{
			kind: item.Kind, name: item.Name, line: item.Line, status: "collision",
			baseCode: item.BaseContent, ourCode: item.OurContent, theirCode: item.TheirContent,
		}
	}

	for _, item := range diff.OurChanges {
		k := item.Kind + ":" + item.Name
		if _, exists := statusMap[k]; !exists {
			statusMap[k] = elemMeta{
				kind: item.Kind, name: item.Name, line: item.Line, status: strings.ToLower(item.Type),
				baseCode: item.BaseContent, ourCode: item.OurContent, theirCode: item.TheirContent,
			}
		}
	}

	for _, item := range diff.TheirChanges {
		k := item.Kind + ":" + item.Name
		if _, exists := statusMap[k]; !exists {
			statusMap[k] = elemMeta{
				kind: item.Kind, name: item.Name, line: item.Line, status: strings.ToLower(item.Type),
				baseCode: item.BaseContent, ourCode: item.OurContent, theirCode: item.TheirContent,
			}
		}
	}

	for key, meta := range statusMap {
		nodeID := sanitizeID(rootID + "__" + key)
		nodeIDByKey[key] = nodeID
		nodes = append(nodes, CyNode{Data: CyNodeData{
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
			key := strings.ToLower(node.Kind) + ":" + node.Name
			if cyID, ok := nodeIDByKey[key]; ok {
				semanticToCyID[node.ID] = cyID
			}
		}

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
			edges = append(edges, CyEdge{Data: CyEdgeData{
				ID:     sanitizeID(edge.ID),
				Source: sourceNodeID,
				Target: targetNodeID,
				Type:   edge.Type,
			}})
		}
	}

	return CyGraph{Elements: CyElements{Nodes: nodes, Edges: edges}}
}

func sanitizeID(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", ".", "_", " ", "_")
	return r.Replace(s)
}

var (
	graphStoreMu sync.Mutex
	graphStore   []CyGraph
)

func RegisterGraph(g CyGraph) {
	graphStoreMu.Lock()
	defer graphStoreMu.Unlock()
	graphStore = append(graphStore, g)
}

func MergedGraph() CyGraph {
	graphStoreMu.Lock()
	defer graphStoreMu.Unlock()

	merged := CyGraph{Elements: CyElements{Nodes: []CyNode{}, Edges: []CyEdge{}}}
	for _, g := range graphStore {
		merged.Elements.Nodes = append(merged.Elements.Nodes, g.Elements.Nodes...)
		merged.Elements.Edges = append(merged.Elements.Edges, g.Elements.Edges...)
	}
	return merged
}

func (g CyGraph) ToDTO() GraphDTO {
	return GraphDTO{
		Nodes: g.Elements.Nodes,
		Edges: g.Elements.Edges,
	}
}

func MergedGraphDTO() GraphDTO {
	return MergedGraph().ToDTO()
}
