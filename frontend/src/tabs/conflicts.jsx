import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import { AlertTriangle, GitCompare, Sparkles, FileText, CheckCircle2 } from 'lucide-react'
import './conflicts.css'

function Conflicts() {
  const [analyses, setAnalyses] = useState([])
  const [selectedFile, setSelectedFile] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const navigate = useNavigate()

  useEffect(() => {
    const controller = new AbortController()

    fetchJson(`${API_BASE}/api/analysis`, { signal: controller.signal })
      .then((payload) => {
        const data = payload.data || []
        setAnalyses(data)
        if (data.length > 0) {
          setSelectedFile(data[0].file)
        }
      })
      .catch((err) => {
        if (isAbort(err)) return
        console.error('Error loading conflicts:', err)
        setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [])

  const selectedAnalysis = analyses.find((analysis) => analysis.file === selectedFile)

  const renderChanges = (title, items) => {
    if (!items || items.length === 0) {
      return <p className="no-changes-note">No {title.toLowerCase()} found.</p>
    }

    return (
      <div className="change-list">
        <h4>{title}</h4>
        {items.map((item, index) => {
          const hasMultipleSides = Boolean(
            (item.base_content && (item.our_content || item.their_content)) ||
            (item.our_content && item.their_content)
          )

          return (
            <div className="change-item" key={`${item.kind}-${item.name}-${index}`}>
              <div className="change-item-header">
                <span className="change-item-title">{item.kind} {item.name}</span>
                <span className="change-item-meta">{item.type} · line {item.line}</span>
              </div>
              <div className={hasMultipleSides ? "code-comparison-grid" : "code-block"}>
                {item.base_content && (
                  <div className="code-pane base-pane">
                    <div className="pane-tag">Base Version</div>
                    <pre><code>{item.base_content}</code></pre>
                  </div>
                )}
                {item.our_content && (
                  <div className="code-pane ours-pane">
                    <div className="pane-tag">Ours Version</div>
                    <pre><code>{item.our_content}</code></pre>
                  </div>
                )}
                {item.their_content && (
                  <div className="code-pane theirs-pane">
                    <div className="pane-tag">Theirs Version</div>
                    <pre><code>{item.their_content}</code></pre>
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    )
  }

  if (loading) {
    return <div className="conflicts-page"><h2>Loading conflict analysis...</h2></div>
  }

  if (error) {
    return <div className="conflicts-page"><h2>Error loading conflict analysis: {error}</h2></div>
  }

  return (
    <div className="conflicts-page">
      <h1>Merge Conflicts</h1>
      <p className="conflicts-subtitle">Detailed structural collisions and per-side modifications.</p>

      {analyses.length === 0 ? (
        <div className="empty-conflicts-box">
          <CheckCircle2 size={32} style={{ color: '#16a34a', marginBottom: '12px' }} />
          <h3>No conflicted files detected</h3>
          <p>Your working tree has no unmerged conflict files.</p>
        </div>
      ) : (
        <div className="conflict-grid">
          <aside className="conflict-list" role="tablist" aria-label="Conflicted files">
            {analyses.map((analysis) => (
              <button
                key={analysis.file}
                className={`conflict-list-item ${analysis.file === selectedFile ? 'active' : ''}`}
                role="tab"
                aria-selected={analysis.file === selectedFile}
                onClick={() => setSelectedFile(analysis.file)}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0, overflow: 'hidden' }}>
                  <AlertTriangle size={14} style={{ color: '#ef4444', flexShrink: 0 }} />
                  <span className="conflict-file-name">{analysis.file}</span>
                </div>
                <span className="conflict-badge">
                  {analysis.smartDiff?.collisions?.length ?? 0} collisions
                </span>
              </button>
            ))}
          </aside>

          <main className="conflict-detail">
            {selectedAnalysis ? (
              <>
                <div className="conflict-detail-header">
                  <div>
                    <h2>{selectedAnalysis.file}</h2>
                    <p>{selectedAnalysis.repositorySummary}</p>
                  </div>
                  <div className="conflict-detail-actions">
                    <button onClick={() => navigate(`/treediff?file=${encodeURIComponent(selectedAnalysis.file)}`)}>
                      <GitCompare size={15} /> View TreeDiff
                    </button>
                    <button onClick={() => navigate(`/suggestions?file=${encodeURIComponent(selectedAnalysis.file)}`)}>
                      <Sparkles size={15} /> View Suggestions
                    </button>
                    <button onClick={() => navigate(`/context?file=${encodeURIComponent(selectedAnalysis.file)}`)}>
                      <FileText size={15} /> View AI Context
                    </button>
                  </div>
                </div>

                <div className="conflict-summary">
                  <span>Collisions: <strong>{selectedAnalysis.smartDiff?.collisions?.length ?? 0}</strong></span>
                  <span>Our changes: <strong>{selectedAnalysis.smartDiff?.our_changes?.length ?? 0}</strong></span>
                  <span>Their changes: <strong>{selectedAnalysis.smartDiff?.their_changes?.length ?? 0}</strong></span>
                </div>

                <section>
                  <h3>Collision details</h3>
                  {renderChanges('Collisions', selectedAnalysis.smartDiff?.collisions)}
                </section>

                <section>
                  <h3>Our changes</h3>
                  {renderChanges('Our Changes', selectedAnalysis.smartDiff?.our_changes)}
                </section>

                <section>
                  <h3>Their changes</h3>
                  {renderChanges('Their Changes', selectedAnalysis.smartDiff?.their_changes)}
                </section>
              </>
            ) : (
              <p>Select a conflict file to view details.</p>
            )}
          </main>
        </div>
      )}
    </div>
  )
}

export default Conflicts
