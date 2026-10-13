import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import { GitCompare, Layers, AlertCircle } from 'lucide-react'
import './treediff.css'

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

    fetchJson(url, { signal: controller.signal })
      .then((payload) => {
        const data = Array.isArray(payload.data) ? payload.data : [payload.data]
        setAnalyses(data.filter(Boolean))
      })
      .catch((err) => {
        if (isAbort(err)) return
        console.error('Error loading TreeDiff:', err)
        setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [file])

  if (loading) {
    return <div className="treediff-page"><h2>Loading TreeDiff...</h2></div>
  }

  if (error) {
    return <div className="treediff-page"><h2>Error loading TreeDiff: {error}</h2></div>
  }

  if (!analyses.length) {
    return (
      <div className="treediff-page">
        <div className="empty-diff-box">
          <AlertCircle size={32} style={{ color: '#94a3b8', marginBottom: '8px' }} />
          <h2>No TreeDiff data available.</h2>
          <p>Scan a repository with active conflicts to view AST node diffs.</p>
        </div>
      </div>
    )
  }

  const renderASTSection = (title, ast, tone) => {
    if (!ast) {
      return (
        <div className={`ast-section ${tone}-section`}>
          <h3>{title}</h3>
          <p className="no-ast-note">No AST nodes extracted.</p>
        </div>
      )
    }

    const functions = ast.functions || []
    const variables = ast.variables || []

    return (
      <div className={`ast-section ${tone}-section`}>
        <div className="ast-section-header">
          <h3>{title}</h3>
          <span className="ast-count-tag">{functions.length + variables.length} symbols</span>
        </div>
        <div className="ast-nodes">
          {functions.length === 0 && variables.length === 0 ? (
            <p className="no-ast-note">No symbols in this version.</p>
          ) : null}

          {functions.map((fn, index) => (
            <div className="ast-node" key={`${fn.name}-${index}`}>
              <div className="ast-node-title">
                <span className="ast-kind">{fn.kind}</span>
                <strong>{fn.name}</strong>
                <span className="ast-line">line {fn.line}</span>
              </div>
              <pre><code>{fn.content}</code></pre>
            </div>
          ))}

          {variables.map((variable, index) => (
            <div className="ast-node" key={`${variable.name}-${index}`}>
              <div className="ast-node-title">
                <span className="ast-kind">{variable.kind}</span>
                <strong>{variable.name}</strong>
                <span className="ast-line">line {variable.line}</span>
              </div>
              <pre><code>{variable.content}</code></pre>
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="treediff-page">
      <div className="treediff-page-header">
        <h1>TreeDiff {file ? `for ${file}` : 'for all conflicted files'}</h1>
        <p>Structural AST node differences extracted via Tree-Sitter grammars.</p>
      </div>

      {analyses.map((analysis) => {
        const baseAst = analysis.baseAst || analysis.BaseAST
        const ourAst = analysis.ourAst || analysis.OurAST
        const theirAst = analysis.theirAst || analysis.TheirAST

        return (
          <div className="file-diff-card" key={analysis.file}>
            <div className="file-diff-header">
              <div>
                <h2>{analysis.file}</h2>
                <p>{analysis.repositorySummary}</p>
              </div>
              <div className="file-stats">
                <span>Collisions: <strong>{analysis.smartDiff?.collisions?.length ?? 0}</strong></span>
                <span>Our Changes: <strong>{analysis.smartDiff?.our_changes?.length ?? 0}</strong></span>
                <span>Their Changes: <strong>{analysis.smartDiff?.their_changes?.length ?? 0}</strong></span>
              </div>
            </div>

            <section className="ast-tree-section">
              <div className="section-title-row">
                <GitCompare size={18} />
                <h3>AST Difference Collisions</h3>
              </div>
              <div className="ast-tree">
                <div className="tree-root">{analysis.file}</div>
                <div className="tree-branches">
                  {analysis.smartDiff?.collisions?.length === 0 ? (
                    <p className="no-ast-note" style={{ padding: '16px' }}>No direct symbol collisions in this file.</p>
                  ) : (
                    analysis.smartDiff?.collisions?.map((collision, index) => (
                      <div className="tree-node" key={`${analysis.file}-${collision.name}-${index}`}>
                        <div className="tree-node-header">
                          <span className="tree-node-label">{collision.kind} {collision.name}</span>
                          <span className="tree-node-badge">Collision · line {collision.line}</span>
                        </div>
                        <div className="tree-node-children">
                          {collision.base_content && (
                            <div className="tree-node-branch base-branch">
                              <strong>Base</strong>
                              <pre>{collision.base_content}</pre>
                            </div>
                          )}
                          {collision.our_content && (
                            <div className="tree-node-branch ours-branch">
                              <strong>Ours</strong>
                              <pre>{collision.our_content}</pre>
                            </div>
                          )}
                          {collision.their_content && (
                            <div className="tree-node-branch theirs-branch">
                              <strong>Theirs</strong>
                              <pre>{collision.their_content}</pre>
                            </div>
                          )}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </section>

            <section className="ast-comparison-section">
              <div className="section-title-row">
                <Layers size={18} />
                <h3>Complete AST Extracted Declarations</h3>
              </div>
              <div className="ast-comparison-grid">
                {renderASTSection('Base AST', baseAst, 'base')}
                {renderASTSection('Ours AST', ourAst, 'ours')}
                {renderASTSection('Theirs AST', theirAst, 'theirs')}
              </div>
            </section>
          </div>
        )
      })}
    </div>
  )
}

export default Treediff