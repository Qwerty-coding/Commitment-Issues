import './dashboard.css'
import { useEffect, useState } from 'react'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import { GitBranch, ArrowRight, Code2, AlertTriangle, Layers } from 'lucide-react'

function Dashboard() {
  const [repoName, setRepoName] = useState("")
  const [currentBranch, setCurrentBranch] = useState("")
  const [incomingBranch, setIncomingBranch] = useState("")
  const [analysis, setAnalysis] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [refreshing, setRefreshing] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    Promise.all([
      fetchJson(`${API_BASE}/api/repository`, { signal: controller.signal })
        .then((repoPayload) => {
          const data = repoPayload?.data || {}
          setRepoName(data.name || "")
          setCurrentBranch(data.currentBranch || "")
          setIncomingBranch(data.incomingBranch || "")
        })
        .catch((err) => {
          if (isAbort(err)) return
          console.warn('Error fetching repository metadata:', err)
        }),
      fetchJson(`${API_BASE}/api/analysis`, { signal: controller.signal })
        .then((analysisPayload) => {
          setAnalysis(analysisPayload.data || [])
        })
        .catch((err) => {
          if (isAbort(err)) return
          console.error('Error fetching repository conflict analysis:', err)
          setError(err.message)
        }),
    ]).finally(() => {
      if (!controller.signal.aborted) {
        setLoading(false)
      }
    })

    return () => controller.abort()
  }, [reloadKey])

  useEffect(() => {
    const onRefreshEvent = () => setReloadKey((k) => k + 1)
    window.addEventListener('ci:refresh-analysis', onRefreshEvent)
    return () => window.removeEventListener('ci:refresh-analysis', onRefreshEvent)
  }, [])

  const handleRefresh = async () => {
    setRefreshing(true)
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
    } finally {
      setRefreshing(false)
    }
  }

  if (loading) {
    return (
      <div className="dashboard-page">
        <h2>Loading dashboard analysis...</h2>
      </div>
    )
  }

  if (error) {
    return (
      <div className="dashboard-page">
        <h2>Error loading dashboard: {error}</h2>
      </div>
    )
  }

  const totalCollisions = analysis.reduce((sum, item) => sum + (item.smartDiff?.collisions?.length || 0), 0)

  return (
    <div className="dashboard-page">
      <div className="welcome-section" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
        <div>
          <h1>Welcome back</h1>
          <p>AST structural diffs, active collisions, and workspace merge status.</p>
        </div>
        <button
          type="button"
          onClick={handleRefresh}
          disabled={refreshing}
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
          {refreshing ? 'Refreshing…' : 'Refresh Analysis'}
        </button>
      </div>

      <div className="repository-card">
        <div className="repository-content">
          <h2>{repoName || 'Repository'}</h2>

          <div className="branch-flow">
            <div className="branch current-branch">
              <GitBranch size={16} />
              <span>{currentBranch || 'unknown'}</span>
            </div>

            <ArrowRight size={16} className="branch-arrow" />

            <div className="branch main-branch">
              <GitBranch size={16} />
              <span>{incomingBranch || 'incoming'}</span>
            </div>
          </div>

          <div className="repository-divider"></div>
          <div className="workspace-text">
            <Code2 size={16} />
            <span>Active merge workspace</span>
          </div>
        </div>
      </div>

      <div className="dashboard-summary">
        <div className="summary-card">
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <AlertTriangle size={18} style={{ color: '#ef4444' }} />
            <h3>Conflicted Files</h3>
          </div>
          <span>{analysis.length}</span>
        </div>
        <div className="summary-card">
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <Layers size={18} style={{ color: 'var(--brand)' }} />
            <h3>Structural Collisions</h3>
          </div>
          <span>{totalCollisions}</span>
        </div>
      </div>

      <div className="analysis-grid">
        {analysis.length === 0 ? (
          <div className="empty-state">
            <h3>No conflicted files found.</h3>
            <p>Run the serve command in a repository with active merge conflicts.</p>
          </div>
        ) : (
          analysis.map((item, fileIndex) => (
            <div className="analysis-card" key={`${item.file}-${fileIndex}`}>
              <div className="analysis-card-header">
                <div>
                  <h3>{item.file}</h3>
                  <p>{item.repositorySummary}</p>
                </div>
                <div className="analysis-counts">
                  <span>Collisions: {item.smartDiff?.collisions?.length ?? 0}</span>
                  <span>Ours: {item.smartDiff?.our_changes?.length ?? 0}</span>
                  <span>Theirs: {item.smartDiff?.their_changes?.length ?? 0}</span>
                </div>
              </div>

              {item.smartDiff?.collisions?.length > 0 ? (
                <div className="analysis-preview">
                  <h4>AST Collision Preview</h4>
                  {item.smartDiff.collisions.map((collision, index) => {
                    const hasMultipleSides = Boolean(
                      collision.base_content &&
                        (collision.our_content || collision.their_content),
                    ) || Boolean(collision.our_content && collision.their_content)
                    return (
                      <div className="preview-row" key={`${collision.kind || 'item'}-${collision.name || collision.line || index}-${index}`}>
                        <strong>{collision.kind} {collision.name}</strong>
                        <span>{collision.type}</span>
                        {hasMultipleSides ? (
                          <div className="preview-sides">
                            {collision.base_content && (
                              <div className="preview-side">
                                <em>Base</em>
                                <pre>{collision.base_content}</pre>
                              </div>
                            )}
                            {collision.our_content && (
                              <div className="preview-side">
                                <em>Ours</em>
                                <pre>{collision.our_content}</pre>
                              </div>
                            )}
                            {collision.their_content && (
                              <div className="preview-side">
                                <em>Theirs</em>
                                <pre>{collision.their_content}</pre>
                              </div>
                            )}
                          </div>
                        ) : (
                          <pre>{collision.base_content || collision.our_content || collision.their_content}</pre>
                        )}
                      </div>
                    )
                  })}
                </div>
              ) : (
                <div className="analysis-preview empty-preview">
                  <h4>No collisions detected in this file</h4>
                </div>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

export default Dashboard