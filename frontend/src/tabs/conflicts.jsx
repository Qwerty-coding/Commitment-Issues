import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import Highlight from '../components/Highlight'
import { languageForFile } from '../lib/highlightLang'
import { AlertTriangle, CheckCircle2 } from 'lucide-react'
import './conflicts.css'

function Conflicts() {
  const [analyses, setAnalyses] = useState([])
  const [selectedFile, setSelectedFile] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [reloadKey, setReloadKey] = useState(0)
  const navigate = useNavigate()

  useEffect(() => {
    const controller = new AbortController()

    fetchJson(`${API_BASE}/api/analysis`, { signal: controller.signal })
      .then((payload) => {
        const data = payload.data || []
        setAnalyses(data)
        if (data.length > 0) {
          setSelectedFile((prev) => (prev && data.some((a) => a.file === prev) ? prev : data[0].file))
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
  }, [reloadKey])

  useEffect(() => {
    const onRefresh = () => setReloadKey((k) => k + 1)
    window.addEventListener('ci:refresh-analysis', onRefresh)
    return () => window.removeEventListener('ci:refresh-analysis', onRefresh)
  }, [])

  const handleRefresh = async () => {
    setLoading(true)
    setError('')
    try {
      await fetchJson(`${API_BASE}/api/analysis/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repositoryRoot: '.', file: '' }),
      })
      setReloadKey((k) => k + 1)
    } catch (err) {
      setError(err.message)
      setLoading(false)
    }
  }

  // Keyboard triage: j / ArrowDown selects the next conflict file, k /
  // ArrowUp the previous. Shortcuts are ignored while the user is typing in
  // an input, textarea or contenteditable so they never hijack typing.
  useEffect(() => {
    const onKeyDown = (event) => {
      const target = event.target
      const tag = target?.tagName?.toLowerCase()
      if (tag === 'input' || tag === 'textarea' || tag === 'select' || target?.isContentEditable) {
        return
      }
      if (event.key !== 'j' && event.key !== 'k' && event.key !== 'ArrowDown' && event.key !== 'ArrowUp') {
        return
      }
      if (analyses.length === 0) return
      event.preventDefault()
      const currentIndex = analyses.findIndex((a) => a.file === selectedFile)
      const forward = event.key === 'j' || event.key === 'ArrowDown'
      let nextIndex = forward ? currentIndex + 1 : currentIndex - 1
      if (nextIndex < 0) nextIndex = analyses.length - 1
      if (nextIndex >= analyses.length) nextIndex = 0
      setSelectedFile(analyses[nextIndex].file)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [analyses, selectedFile])

  const selectedAnalysis = analyses.find((analysis) => analysis.file === selectedFile)
  const selectedLanguage = languageForFile(selectedFile)

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
                    <pre><Highlight code={item.base_content} language={selectedLanguage} /></pre>
                  </div>
                )}
                {item.our_content && (
                  <div className="code-pane ours-pane">
                    <div className="pane-tag">Ours Version</div>
                    <pre><Highlight code={item.our_content} language={selectedLanguage} /></pre>
                  </div>
                )}
                {item.their_content && (
                  <div className="code-pane theirs-pane">
                    <div className="pane-tag">Theirs Version</div>
                    <pre><Highlight code={item.their_content} language={selectedLanguage} /></pre>
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
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
        <h1>Merge Conflicts</h1>
        <button
          type="button"
          onClick={handleRefresh}
          style={{
            padding: '7px 14px',
            fontSize: '13px',
            fontWeight: 500,
            borderRadius: '8px',
            border: '1px solid var(--card-border, #dde2ea)',
            background: 'var(--card-bg, #ffffff)',
            color: 'var(--text-primary, #101828)',
            cursor: 'pointer',
          }}
        >
          Refresh Analysis
        </button>
      </div>
      <p className="conflicts-subtitle">
        Detailed structural collisions and per-side modifications.
        {' '}<span className="conflicts-shortcut-hint">Tip: press <kbd>j</kbd>/<kbd>k</kbd> to move between files.</span>
      </p>

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
                      View TreeDiff
                    </button>
                    <button onClick={() => navigate(`/suggestions?file=${encodeURIComponent(selectedAnalysis.file)}`)}>
                      View Suggestions
                    </button>
                    <button onClick={() => navigate(`/context?file=${encodeURIComponent(selectedAnalysis.file)}`)}>
                      View AI Context
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
