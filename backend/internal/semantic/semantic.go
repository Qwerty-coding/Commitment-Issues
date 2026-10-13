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
	// File is the source file, used as the second deterministic sort key.
	File string `json:"file,omitempty"`
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
		item.File = elementFile(baseEl, sideEl)
		item.BaseContent = baseEl.Content
		item.Identity = SymbolIdentity(baseEl)
		return item
	}

	item.Kind, item.Name, item.Line = sideEl.Kind, sideEl.Name, sideEl.Line
	item.File = elementFile(baseEl, sideEl)
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

// elementFile picks the source file for a diff item, preferring the side that
// actually carries the change and falling back to the base element.
func elementFile(baseEl, sideEl parser.CodeElement) string {
	if sideEl.File != "" {
		return sideEl.File
	}
	return baseEl.File
}

// CompareDiffItems defines the exact deterministic ordering used for every
// semantic output collection:
//
//	identity -> file -> kind -> name -> line -> type
func CompareDiffItems(a, b DiffItem) int {
	if c := strings.Compare(a.Identity, b.Identity); c != 0 {
		return c
	}
	if c := strings.Compare(a.File, b.File); c != 0 {
		return c
	}
	if c := strings.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	if c := strings.Compare(a.Name, b.Name); c != 0 {
		return c
	}
	if a.Line != b.Line {
		if a.Line < b.Line {
			return -1
		}
		return 1
	}
	return strings.Compare(a.Type, b.Type)
}

// SortDiffItems sorts a slice in place using the canonical deterministic order.
func SortDiffItems(items []DiffItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return CompareDiffItems(items[i], items[j]) < 0
	})
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
	result := GenerateSmartDiff(ctx, base, ours, theirs)
	if err := ctx.Err(); err != nil {
		return SmartDiffResult{}, err
	}
	return result, nil
}

// DeclarationKey matches code elements across signature changes: it uses
// every stable identity field EXCEPT the signature, so a function whose
// parameter list was edited on one or both branches keeps a single logical
// identity (file + scope + kind + name).
func DeclarationKey(el parser.CodeElement) string {
	return fmt.Sprintf("%s|%s|%s|%s", el.File, el.Scope, el.Kind, el.Name)
}

func GenerateSmartDiff(ctx context.Context, base, ours, theirs parser.ASTContext) SmartDiffResult {
	baseMap := BuildSignatureMap(base)
	oursMap := BuildSignatureMap(ours)
	theirsMap := BuildSignatureMap(theirs)

	signatures := make(map[string]struct{}, len(oursMap)+len(theirsMap))
	for sig := range oursMap {
		if ctx.Err() != nil {
			return SmartDiffResult{}
		}
		signatures[sig] = struct{}{}
	}
	for sig := range theirsMap {
		if ctx.Err() != nil {
			return SmartDiffResult{}
		}
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
				File:        elementFile(baseEl, ourEl),
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

	// Second pass: match declarations whose signature changed, which the
	// identity-keyed pass above is blind to. When the same declaration (same
	// file, scope, kind and name) was edited on BOTH sides with differing
	// results — including signature edits — it is one structural collision,
	// not a false "delete + independent add" pair.
	result = reconcileDeclarationCollisions(result, base, ours, theirs)

	// Every collection is emitted in the canonical deterministic order so that
	// map iteration order can never influence output.
	SortDiffItems(result.OurChanges)
	SortDiffItems(result.TheirChanges)
	SortDiffItems(result.Collisions)

	return result
}

// reconcileDeclarationCollisions detects structural collisions on
// declarations whose SIGNATURE changed on both branches — the case the
// identity-keyed diff above is blind to (base identity vanishes on both
// sides, which otherwise renders as a false "deleted + two independent adds"
// pair).
//
// The precise rule: for a declaration (same file, scope, kind, name), the
// base must hold exactly one element, both sides must hold exactly one
// element whose identity is NOT in the base, and the two new-identity
// elements must differ. That single logical change is reported as ONE
// collision (Identity = "decl|" + DeclarationKey) and the false per-identity
// ADDED/DELETED entries are removed from both change lists.
func reconcileDeclarationCollisions(result SmartDiffResult, base, ours, theirs parser.ASTContext) SmartDiffResult {
	type group struct {
		base, our, their map[string]parser.CodeElement // identity -> element
	}
	decls := make(map[string]*group)
	ensure := func(k string) *group {
		g, ok := decls[k]
		if !ok {
			g = &group{base: map[string]parser.CodeElement{}, our: map[string]parser.CodeElement{}, their: map[string]parser.CodeElement{}}
			decls[k] = g
		}
		return g
	}
	for _, el := range base.Functions {
		ensure(DeclarationKey(el)).base[SymbolIdentity(el)] = el
	}
	for _, el := range ours.Functions {
		ensure(DeclarationKey(el)).our[SymbolIdentity(el)] = el
	}
	for _, el := range theirs.Functions {
		ensure(DeclarationKey(el)).their[SymbolIdentity(el)] = el
	}

	// Deterministic iteration: keys sorted ascending.
	keys := make([]string, 0, len(decls))
	for k := range decls {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		g := decls[key]
		if len(g.base) != 1 || len(g.our) != 1 || len(g.their) != 1 {
			// Overloads, multiple base entries or one-sided declarations are
			// matched by identity; grouping them differently could merge
			// genuinely distinct symbols.
			continue
		}
		var baseEl, ourEl, theirEl parser.CodeElement
		for id, el := range g.base {
			_ = id
			baseEl = el
		}
		var ourNewID, theirNewID string
		for id, el := range g.our {
			ourEl = el
			ourNewID = id
		}
		for id, el := range g.their {
			theirEl = el
			theirNewID = id
		}
		_, ourInBase := g.base[ourNewID]
		_, theirInBase := g.base[theirNewID]
		if ourInBase || theirInBase {
			// At least one side kept the base identity: the identity-keyed pass
			// already handled any collision or update for this declaration.
			continue
		}
		// Element at the same decl key on each side, with distinct identity per
		// side and one base element: identity pass reported base DELETED on both
		// sides plus two independent ADDED entries — the blind spot.
		if ourEl.Content == theirEl.Content {
			continue // both sides converged on the same result
		}

		result.Collisions = append(result.Collisions, DiffItem{
			Type: "COLLISION", Kind: ourEl.Kind, Name: ourEl.Name, Line: ourEl.Line,
			File:        elementFile(baseEl, ourEl),
			BaseContent: baseEl.Content, OurContent: ourEl.Content, TheirContent: theirEl.Content,
			Identity: "decl|" + key,
		})

		// Drop the false identity-pass entries this collision replaces: the
		// base (reported deleted on both sides) and the two add-pairs.
		dropOurs := map[string]struct{}{SymbolIdentity(baseEl): {}, ourNewID: {}}
		dropTheirs := map[string]struct{}{SymbolIdentity(baseEl): {}, theirNewID: {}}
		result.OurChanges = dropIdentityEntries(result.OurChanges, dropOurs, ourEl)
		result.TheirChanges = dropIdentityEntries(result.TheirChanges, dropTheirs, theirEl)
	}

	return result
}

// dropIdentityEntries removes change entries matching one of the given
// identities, or any entry for the same declaration (kind+name).
func dropIdentityEntries(items []DiffItem, dropIDs map[string]struct{}, ref parser.CodeElement) []DiffItem {
	kept := items[:0]
	for _, item := range items {
		if _, drop := dropIDs[item.Identity]; drop {
			continue
		}
		if strings.EqualFold(item.Kind, ref.Kind) && item.Name == ref.Name {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

// callableKind reports whether a symbol kind can carry CALLS edges in the
// semantic call graph. Methods, constructors and functions are callable;
// classes, interfaces, structs, enums, modules, files and variables are not.
func callableKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "function", "method", "constructor":
		return true
	default:
		return false
	}
}

// callableKind is the SemanticNode method form of callableKind.
func (n SemanticNode) callableKind() bool {
	return callableKind(n.Kind)
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
	if node.callableKind() {
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
		if !callableKind(fn.Kind) {
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

	// Emit nodes and edges in a stable order so graph-derived results are
	// independent of map iteration order.
	sortSemanticGraph(&result)

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

	sortSemanticGraph(&merged)

	return merged
}

// sortSemanticGraph orders nodes and edges by ID for deterministic output.
func sortSemanticGraph(g *SemanticGraph) {
	sort.SliceStable(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.SliceStable(g.Edges, func(i, j int) bool { return g.Edges[i].ID < g.Edges[j].ID })
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

	// Seed traversal from every collision, regardless of symbol kind (functions,
	// methods, constructors, classes, variables, files and future kinds).
	//
	// When a collision carries a precise identity, only that precise identity is
	// used — there is no fallback to kind:name, because falling back could match
	// an unrelated same-named symbol from another scope/file. Fallback to the
	// legacy kind:name key is only permitted when the collision itself lacks a
	// precise identity, which keeps the old behaviour for legacy/simple elements.
	seedCollisions := append([]DiffItem(nil), collisions...)
	sort.SliceStable(seedCollisions, func(i, j int) bool {
		return CompareDiffItems(seedCollisions[i], seedCollisions[j]) < 0
	})
	for _, collision := range seedCollisions {
		var root SemanticNode
		var found bool
		if collision.Identity != "" {
			// Precise identity from the collision must match exactly.
			root, found = nodeByKey[collision.Identity]
		} else {
			// Collision has no identity: use DiffKey(collision) first, then fall
			// back to the legacy kind:name key for the collision.
			root, found = nodeByKey[DiffKey(collision)]
			if !found {
				root, found = nodeByKey[semanticNodeKey(collision.Kind, collision.Name)]
			}
		}
		if found {
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
