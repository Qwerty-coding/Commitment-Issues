package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"CommitIssues/internal/graph"
	"CommitIssues/internal/runstate"
)

// registerGraphInspectHandlers implements the previously stubbed
// /api/graph/focus, /api/graph/expand and /api/graph/collapse endpoints on
// top of the run's merged graph. Every response is a standard GraphDTO.
//
//	focus  ?id=N  -> the node, its direct edges and its neighbors
//	expand ?id=N  -> the node + neighbors it is not yet rendered with
//	                 (same payload as focus; the client merges)
//	collapse?id=N -> the graph without the node's downstream closure
func registerGraphInspectHandlers(mux *http.ServeMux, run *runstate.Run) {
	handle := func(w http.ResponseWriter, r *http.Request, mode string) {
		w.Header().Set("Access-Control-Allow-Origin", "*")

		nodeID := r.URL.Query().Get("id")
		if nodeID == "" && r.Method == http.MethodPost {
			var body struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				nodeID = strings.TrimSpace(body.ID)
			}
		}
		if nodeID == "" {
			writeError(w, http.StatusBadRequest, "MISSING_NODE_ID", "query parameter id is required")
			return
		}

		merged := run.MergedGraph()
		nodes, edges := inspectGraph(merged, nodeID, mode)
		if len(nodes) == 0 {
			writeError(w, http.StatusNotFound, "NODE_NOT_FOUND", fmt.Sprintf("node %q not found in the merged graph", nodeID))
			return
		}
		writeJSON(w, http.StatusOK, standardResponse{Success: true, Message: mode, Data: graph.GraphDTO{Nodes: nodes, Edges: edges}})
	}

	mux.HandleFunc("/api/graph/focus", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, "focus")
	})
	mux.HandleFunc("/api/graph/expand", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, "expand")
	})
	mux.HandleFunc("/api/graph/collapse", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, "collapse")
	})
}

// inspectGraph builds the node/edge set for one inspect mode.
func inspectGraph(merged graph.CyGraph, nodeID, mode string) ([]graph.CyNode, []graph.CyEdge) {
	type adjacency struct {
		outgoing, incoming []graph.CyEdge
	}
	adj := make(map[string]*adjacency)
	nodeByID := make(map[string]graph.CyNode, len(merged.Elements.Nodes))
	for _, n := range merged.Elements.Nodes {
		nodeByID[n.Data.ID] = n
	}
	for _, e := range merged.Elements.Edges {
		a, ok := adj[e.Data.Source]
		if !ok {
			a = &adjacency{}
			adj[e.Data.Source] = a
		}
		a.outgoing = append(a.outgoing, e)
		b, ok := adj[e.Data.Target]
		if !ok {
			b = &adjacency{}
			adj[e.Data.Target] = b
		}
		b.incoming = append(b.incoming, e)
	}

	center, exists := nodeByID[nodeID]
	if !exists {
		return nil, nil
	}

	includeNode := func(id string) (graph.CyNode, bool) {
		n, ok := nodeByID[id]
		return n, ok
	}

	switch mode {
	case "collapse":
		// Everything except the center's downstream closure: BFS outward
		// through outgoing edges (which would expand the subtree) and drop
		// those nodes plus their incident edges.
		excluded := make(map[string]struct{})
		queue := []string{nodeID}
		excluded[nodeID] = struct{}{}
		edgeIDsDropped := make(map[string]struct{})
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			a := adj[current]
			if a == nil {
				continue
			}
			for _, e := range a.outgoing {
				edgeIDsDropped[e.Data.ID] = struct{}{}
				if _, seen := excluded[e.Data.Target]; seen {
					continue
				}
				if _, ok := nodeByID[e.Data.Target]; !ok {
					continue
				}
				excluded[e.Data.Target] = struct{}{}
				queue = append(queue, e.Data.Target)
			}
		}
		nodes := make([]graph.CyNode, 0, len(nodeByID))
		for _, n := range merged.Elements.Nodes {
			if _, drop := excluded[n.Data.ID]; drop {
				continue
			}
			nodes = append(nodes, n)
		}
		edges := make([]graph.CyEdge, 0, len(merged.Elements.Edges))
		for _, e := range merged.Elements.Edges {
			if _, drop := edgeIDsDropped[e.Data.ID]; drop {
				continue
			}
			if _, dropS := excluded[e.Data.Source]; dropS {
				continue
			}
			if _, dropT := excluded[e.Data.Target]; dropT {
				continue
			}
			edges = append(edges, e)
		}
		return nodes, edges
	default: // focus / expand: closed neighborhood of the center.
		a := adj[nodeID]
		var edges []graph.CyEdge
		neighborIDs := make(map[string]struct{})
		if a != nil {
			for _, e := range a.outgoing {
				edges = append(edges, e)
				neighborIDs[e.Data.Target] = struct{}{}
			}
			for _, e := range a.incoming {
				edges = append(edges, e)
				neighborIDs[e.Data.Source] = struct{}{}
			}
		}
		nodes := []graph.CyNode{center}
		for id := range neighborIDs {
			if n, ok := includeNode(id); ok {
				nodes = append(nodes, n)
			}
		}
		return nodes, edges
	}
}
