import { useEffect, useState } from 'react'
import './commitgraph.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function Commitgraph() {
  const [graph, setGraph] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    fetch(`${API_BASE}/api/graph`)
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load graph')
        }
        setGraph(payload.data)
      })
      .catch((err) => {
        console.error('Error loading graph:', err)
        setError(err.message)
      })
      .finally(() => setLoading(false))
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
    const childMap = graph.edges.reduce((acc, edge) => {
      const source = edge.data.source
      acc[source] = acc[source] || []
      acc[source].push(edge.data.target)
      return acc
    }, {})

    const nodeById = graph.nodes.reduce((acc, node) => {
      acc[node.data.id] = node
      return acc
    }, {})

    const renderNode = (nodeId) => {
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
              {childMap[nodeId].map((childId) => renderNode(childId))}
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
            {rootNodes.map((node) => renderNode(node.data.id))}
          </ul>
        ) : (
          <div className="graph-card-empty">
            <p>No file nodes found. Showing all nodes instead.</p>
            <ul className="tree-view">
              {graph.nodes.map((node) => renderNode(node.data.id))}
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