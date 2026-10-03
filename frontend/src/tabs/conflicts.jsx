import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import './conflicts.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function Conflicts() {
  const [analyses, setAnalyses] = useState([])
  const [selectedFile, setSelectedFile] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const navigate = useNavigate()

  useEffect(() => {
    fetch(`${API_BASE}/api/analysis`)
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load analysis')
        }
        const data = payload.data || []
        setAnalyses(data)
        if (data.length > 0) {
          setSelectedFile(data[0].file)
        }
      })
      .catch((err) => {
        console.error('Error loading conflicts:', err)
        setError(err.message)
      })
      .finally(() => setLoading(false))
  }, [])

  const selectedAnalysis = analyses.find((analysis) => analysis.file === selectedFile)

  const renderChanges = (title, items) => {
    if (!items || items.length === 0) {
      return <p>No {title.toLowerCase()} found.</p>
    }

    return (
      <div className="change-list">
        <h4>{title}</h4>
        {items.map((item, index) => (
          <div className="change-item" key={`${item.kind}-${item.name}-${index}`}>
            <div className="change-item-header">
              <span className="change-item-title">{item.kind} {item.name}</span>
              <span className="change-item-meta">{item.type} · line {item.line}</span>
            </div>
            <div className="code-block">
              {item.base_content ? <div><strong>Base</strong><pre><code>{item.base_content}</code></pre></div> : null}
              {item.our_content ? <div><strong>Ours</strong><pre><code>{item.our_content}</code></pre></div> : null}
              {item.their_content ? <div><strong>Theirs</strong><pre><code>{item.their_content}</code></pre></div> : null}
            </div>
          </div>
        ))}
      </div>
    )
  }

  if (loading) {
    return <h2>Loading conflict analysis...</h2>
  }

  if (error) {
    return <h2>Error loading conflict analysis: {error}</h2>
  }

  return (
    <div className="conflicts-page">
      <h1>Conflicts</h1>

      {analyses.length === 0 ? (
        <p>No conflict analysis available.</p>
      ) : (
        <div className="conflict-grid">
          <aside className="conflict-list">
            {analyses.map((analysis) => (
              <button
                key={analysis.file}
                className={`conflict-list-item ${analysis.file === selectedFile ? 'active' : ''}`}
                onClick={() => setSelectedFile(analysis.file)}
              >
                <span>{analysis.file}</span>
                <span>{analysis.smartDiff?.collisions?.length ?? 0} conflicts</span>
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
                      View AST TreeDiff
                    </button>
                    <button onClick={() => navigate(`/suggestions?file=${encodeURIComponent(selectedAnalysis.file)}`)}>
                      View Suggestions
                    </button>
                  </div>
                </div>

                <div className="conflict-summary">
                  <span>Collisions: {selectedAnalysis.smartDiff?.collisions?.length ?? 0}</span>
                  <span>Our changes: {selectedAnalysis.smartDiff?.our_changes?.length ?? 0}</span>
                  <span>Their changes: {selectedAnalysis.smartDiff?.their_changes?.length ?? 0}</span>
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
