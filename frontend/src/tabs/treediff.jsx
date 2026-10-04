import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import './treediff.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function Treediff() {
  const location = useLocation()
  const query = new URLSearchParams(location.search)
  const file = query.get('file') || ''

  const [analyses, setAnalyses] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    const url = file ? `${API_BASE}/api/analysis?file=${encodeURIComponent(file)}` : `${API_BASE}/api/analysis`

    fetch(url, { signal: controller.signal })
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load analysis')
        }
        const data = Array.isArray(payload.data) ? payload.data : [payload.data]
        setAnalyses(data)
      })
      .catch((err) => {
        if (err.name === 'AbortError') return
        console.error('Error loading TreeDiff:', err)
        setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [file])

  if (loading) {
    return <h2>Loading TreeDiff...</h2>
  }

  if (error) {
    return <h2>Error loading TreeDiff: {error}</h2>
  }

  if (!analyses.length) {
    return <h2>No TreeDiff data available.</h2>
  }

  const renderASTSection = (title, ast) => {
    if (!ast) {
      return null
    }

    return (
      <div className="ast-section">
        <h3>{title}</h3>
        <div className="ast-nodes">
          {ast.functions?.map((fn, index) => (
            <div className="ast-node" key={`${fn.name}-${index}`}>
              <strong>{fn.kind}</strong> {fn.name} (line {fn.line})
              <pre>{fn.content}</pre>
            </div>
          ))}

          {ast.variables?.map((variable, index) => (
            <div className="ast-node" key={`${variable.name}-${index}`}>
              <strong>{variable.kind}</strong> {variable.name} (line {variable.line})
              <pre>{variable.content}</pre>
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="treediff-page">
      <h1>TreeDiff {file ? `for ${file}` : 'for all conflicted files'}</h1>
      <div className="treediff-summary">
        <span>Files: {analyses.length}</span>
      </div>

      {analyses.map((analysis) => (
        <div className="file-diff-card" key={analysis.file}>
          <div className="file-diff-header">
            <div>
              <h2>{analysis.file}</h2>
              <p>{analysis.repositorySummary}</p>
            </div>
            <div className="file-stats">
              <span>Collisions: {analysis.smartDiff?.collisions?.length ?? 0}</span>
              <span>Our Changes: {analysis.smartDiff?.our_changes?.length ?? 0}</span>
              <span>Their Changes: {analysis.smartDiff?.their_changes?.length ?? 0}</span>
            </div>
          </div>

          <section className="ast-tree-section">
            <h3>AST Difference Tree</h3>
            <div className="ast-tree">
              <div className="tree-root">{analysis.file}</div>
              <div className="tree-branches">
                {analysis.smartDiff?.collisions?.map((collision, index) => (
                  <div className="tree-node" key={`${analysis.file}-${collision.name}-${index}`}>
                    <div className="tree-node-header">
                      <span className="tree-node-label">{collision.kind} {collision.name}</span>
                      <span className="tree-node-badge">Collision</span>
                    </div>
                    <div className="tree-node-children">
                      {collision.base_content ? (
                        <div className="tree-node-branch">
                          <strong>Base</strong>
                          <pre>{collision.base_content}</pre>
                        </div>
                      ) : null}
                      {collision.our_content ? (
                        <div className="tree-node-branch">
                          <strong>Ours</strong>
                          <pre>{collision.our_content}</pre>
                        </div>
                      ) : null}
                      {collision.their_content ? (
                        <div className="tree-node-branch">
                          <strong>Theirs</strong>
                          <pre>{collision.their_content}</pre>
                        </div>
                      ) : null}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </section>

          <section className="ast-comparison-grid">
            {renderASTSection('Base AST', analysis.baseAst)}
            {renderASTSection('Ours AST', analysis.ourAst)}
            {renderASTSection('Theirs AST', analysis.theirAst)}
          </section>
        </div>
      ))}
    </div>
  )
}

export default Treediff