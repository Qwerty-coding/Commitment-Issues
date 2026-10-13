import { useEffect, useState } from 'react'
import './commitgraph.css'

import { API_BASE, fetchJson, isAbort } from '../lib/api'

function Commitgraph() {
  const [graph, setGraph] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()

    fetchJson(`${API_BASE}/api/graph`, { signal: controller.signal })
      .then((payload) => {
        setGraph(payload.data)
      })
      .catch((err) => {
        if (isAbort(err)) return
        console.error('Error loading graph:', err)
        setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [])

  if (loading) {
    return <h2>Loading commit graph...</h2>
  }

  if (error) {
    return <h2>Error loading graph: {error}</h2>
  }

  const renderEdgeList = () => {
    if (!graph?.edges?.length) {
      return null
    }

    return (
      <div className="edge-list-card">
        <h2>Edge relationships</h2>
        <ul>
          {graph.edges.map((edge) => (
            <li key={edge.data.id}>
              <span>{edge.data.source}</span>
              <strong>→</strong>
              <span>{edge.data.target}</span>
              <span className="edge-type">{edge.data.type}</span>
            </li>
          ))}
        </ul>
      </div>
    )
  }

  const renderGraphTree = () => {
    if (!graph || !graph.nodes?.length) {
      return <div className="no-graph">No graph data available.</div>
    }

    const rootNodes = graph.nodes.filter((node) => node.data.status === 'file')
    // Deduplicate targets per source so repeated CALLS edges cannot fan out.
    const childMap = {}
    
    // 1. Add AST structural hierarchy (parent -> child)
    graph.nodes.forEach((node) => {
      const parentId = node.data.parent
      if (parentId) {
        childMap[parentId] = childMap[parentId] || []
        if (!childMap[parentId].includes(node.data.id)) {
          childMap[parentId].push(node.data.id)
        }
      }
    })

    // 2. Add semantic CALLS edges
    if (graph.edges) {
      graph.edges.forEach((edge) => {
        const source = edge.data.source
        const target = edge.data.target
        childMap[source] = childMap[source] || []
        if (!childMap[source].includes(target)) {
          childMap[source].push(target)
        }
      })
    }

    const nodeById = graph.nodes.reduce((acc, node) => {
      acc[node.data.id] = node
      return acc
    }, {})

    // Every node is rendered once per traversal; repeats degrade to a cycle
    // marker instead of recursing forever ("Maximum call stack size exceeded").
    // Each root starts with a fresh rendered set so sibling roots' children are
    // not falsely flagged as cycles.
    const renderFrom = (rendered, nodeId) => {
      if (rendered.has(nodeId)) {
        const seen = nodeById[nodeId]
        return (
          <li className="tree-node tree-node-cycle" key={`${nodeId}-cycle`}>
            <div className="tree-label">
              <div className="node-title">↻ {seen?.data.label || nodeId}</div>
              <div className="node-meta">cycle — already shown above</div>
            </div>
          </li>
        )
      }
      rendered.add(nodeId)

      const node = nodeById[nodeId]
      if (!node) return null

      return (
        <li className="tree-node" key={nodeId}>
          <div className="tree-label">
            <div className="node-title">{node.data.label || node.data.id}</div>
            <div className="node-meta">{node.data.kind} · {node.data.status}</div>
          </div>

          <div className="tree-branch">
            {node.data.base_code ? <div className="tree-leaf"><strong>Base</strong><pre>{node.data.base_code}</pre></div> : null}
            {node.data.our_code ? <div className="tree-leaf"><strong>Ours</strong><pre>{node.data.our_code}</pre></div> : null}
            {node.data.their_code ? <div className="tree-leaf"><strong>Theirs</strong><pre>{node.data.their_code}</pre></div> : null}
          </div>

          {childMap[nodeId]?.length ? (
            <ul className="tree-children">
              {childMap[nodeId].map((childId) => renderFrom(rendered, childId))}
            </ul>
          ) : null}
        </li>
      )
    }

    const hasRoot = rootNodes.length > 0
    return (
      <div className="graph-card">
        {hasRoot ? (
          <ul className="tree-view">
            {rootNodes.map((node) => renderFrom(new Set(), node.data.id))}
          </ul>
        ) : (
          <div className="graph-card-empty">
            <p>No file nodes found. Showing all nodes instead.</p>
            <ul className="tree-view">
              {graph.nodes.map((node) => renderFrom(new Set(), node.data.id))}
            </ul>
          </div>
        )}
        {renderEdgeList()}
      </div>
    )
  }

  return (
    <div className="commitgraph-page">
      <h1>Commit Graph</h1>
      <div className="graph-summary">
        <span>Nodes: {graph?.nodes?.length ?? 0}</span>
        <span>Edges: {graph?.edges?.length ?? 0}</span>
      </div>
      {renderGraphTree()}
    </div>
  )
}

export default Commitgraph;