import { useEffect, useState } from 'react'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import { Network, ArrowRight, Layers } from 'lucide-react'
import './commitgraph.css'

function Commitgraph() {
  const [graph, setGraph] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filterQuery, setFilterQuery] = useState('')
  const [focusedNodeId, setFocusedNodeId] = useState(null)
  const [focusLoading, setFocusLoading] = useState(false)

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

  const handleFocusNode = async (nodeId) => {
    if (focusedNodeId === nodeId) {
      handleResetFocus()
      return
    }
    setFocusLoading(true)
    setError('')
    try {
      const payload = await fetchJson(`${API_BASE}/api/graph/focus?id=${encodeURIComponent(nodeId)}`)
      if (payload.data) {
        setGraph(payload.data)
        setFocusedNodeId(nodeId)
      }
    } catch (err) {
      console.error('Error focusing node:', err)
      setError(`Failed to focus node ${nodeId}: ${err.message}`)
    } finally {
      setFocusLoading(false)
    }
  }

  const handleResetFocus = async () => {
    setFocusLoading(true)
    setError('')
    try {
      const payload = await fetchJson(`${API_BASE}/api/graph`)
      if (payload.data) {
        setGraph(payload.data)
        setFocusedNodeId(null)
      }
    } catch (err) {
      console.error('Error reloading graph:', err)
      setError(`Failed to reload graph: ${err.message}`)
    } finally {
      setFocusLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="commitgraph-page">
        <h2>Loading AST & Dependency Graph...</h2>
      </div>
    )
  }

  if (error) {
    return (
      <div className="commitgraph-page">
        <h2>Error loading graph: {error}</h2>
      </div>
    )
  }

  const renderEdgeList = () => {
    if (!graph?.edges?.length) {
      return null
    }

    const filteredEdges = filterQuery
      ? graph.edges.filter(
          (edge) =>
            edge.data.source.toLowerCase().includes(filterQuery.toLowerCase()) ||
            edge.data.target.toLowerCase().includes(filterQuery.toLowerCase())
        )
      : graph.edges

    return (
      <div className="edge-list-card">
        <div className="edge-list-header">
          <Layers size={18} />
          <h2>Semantic Call Edges ({filteredEdges.length})</h2>
        </div>
        <ul className="edge-items">
          {filteredEdges.map((edge) => (
            <li key={edge.data.id} className="edge-item">
              <span className="edge-source">{edge.data.source}</span>
              <ArrowRight size={14} className="edge-arrow" />
              <span className="edge-target">{edge.data.target}</span>
              <span className="edge-type-badge">{edge.data.type || 'CALLS'}</span>
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

    const rootNodes = focusedNodeId
      ? graph.nodes.filter((node) => node.data.id === focusedNodeId)
      : graph.nodes.filter((node) => node.data.status === 'file')
    const childMap = {}

    // 1. AST structural hierarchy (parent -> child)
    graph.nodes.forEach((node) => {
      const parentId = node.data.parent
      if (parentId) {
        childMap[parentId] = childMap[parentId] || []
        if (!childMap[parentId].includes(node.data.id)) {
          childMap[parentId].push(node.data.id)
        }
      }
    })

    // 2. Semantic CALLS edges
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

    const renderFrom = (rendered, nodeId) => {
      if (rendered.has(nodeId)) {
        const seen = nodeById[nodeId]
        return (
          <li className="tree-node tree-node-cycle" key={`${nodeId}-cycle`}>
            <div className="tree-label">
              <div className="node-title">↻ {seen?.data.label || nodeId}</div>
              <div className="node-meta">Cycle reference — already explored</div>
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
            <div className="node-header-row">
              <span className="node-kind-tag">{node.data.kind || 'Symbol'}</span>
              <span className="node-title">{node.data.label || node.data.id}</span>
              <button
                type="button"
                className={`node-focus-btn ${focusedNodeId === node.data.id ? 'active' : ''}`}
                onClick={() => handleFocusNode(node.data.id)}
                title={focusedNodeId === node.data.id ? 'Click to reset view' : 'Focus on this symbol neighborhood'}
              >
                {focusedNodeId === node.data.id ? 'Focused' : 'Focus'}
              </button>
              <span className="node-status-badge">{node.data.status}</span>
            </div>
          </div>

          <div className="tree-branch">
            {node.data.base_code && (
              <div className="tree-leaf"><strong>Base</strong><pre>{node.data.base_code}</pre></div>
            )}
            {node.data.our_code && (
              <div className="tree-leaf"><strong>Ours</strong><pre>{node.data.our_code}</pre></div>
            )}
            {node.data.their_code && (
              <div className="tree-leaf"><strong>Theirs</strong><pre>{node.data.their_code}</pre></div>
            )}
          </div>

          {childMap[nodeId]?.length ? (
            <ul className="tree-children">
              {childMap[nodeId].map((childId) => renderFrom(rendered, childId))}
            </ul>
          ) : null}
        </li>
      )
    }

    const rendered = new Set()
    const primaryRoots = rootNodes.length > 0 ? rootNodes : graph.nodes
    const primaryTree = primaryRoots.map((node) => renderFrom(rendered, node.data.id))
    const remainingNodes = graph.nodes
      .filter((node) => !rendered.has(node.data.id))
      .map((node) => renderFrom(rendered, node.data.id))

    return (
      <div className="graph-card">
        <div className="graph-card-header">
          <Network size={18} />
          <h2>
            {focusedNodeId
              ? `Focused Neighborhood: ${focusedNodeId}`
              : 'AST Structural Hierarchy & Scope Nodes'}
          </h2>
        </div>
        <ul className="tree-view">
          {primaryTree}
          {remainingNodes}
        </ul>
      </div>
    )
  }

  return (
    <div className="commitgraph-page">
      <div className="commitgraph-header">
        <h1>AST & Dependency Graph</h1>
        <p>Extracted symbol hierarchy, enclosing scopes, and semantic call dependencies across conflicted files.</p>
        <div style={{ marginTop: 12 }}>
          <input
            type="text"
            placeholder="Filter call edges by symbol..."
            value={filterQuery}
            onChange={(e) => setFilterQuery(e.target.value)}
            style={{
              padding: '8px 14px',
              borderRadius: 10,
              border: '1px solid #cbd5e1',
              fontSize: 13,
              width: 280,
              maxWidth: '100%',
            }}
          />
        </div>
        {focusedNodeId && (
          <div className="focus-banner">
            <span>
              Focusing on symbol: <strong>{focusedNodeId}</strong> (showing direct neighborhood)
            </span>
            <button type="button" onClick={handleResetFocus} disabled={focusLoading}>
              {focusLoading ? 'Resetting...' : 'Reset View'}
            </button>
          </div>
        )}
      </div>

      <div className="graph-layout">
        {renderGraphTree()}
        {renderEdgeList()}
      </div>
    </div>
  )
}

export default Commitgraph