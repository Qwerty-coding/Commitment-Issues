import './dashboard.css'
import { useEffect, useState } from 'react'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function Dashboard() {
  const [repoName, setRepoName] = useState("")
  const [currentBranch, setCurrentBranch] = useState("")
  const [incomingBranch, setIncomingBranch] = useState("")
  const [analysis, setAnalysis] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  useEffect(() => {
    const controller = new AbortController()

    fetch(`${API_BASE}/api/repository`, { signal: controller.signal })
      .then((response) => response.json())
      .then((payload) => {
        const data = payload?.data || {}
        setRepoName(data.name || "")
        setCurrentBranch(data.currentBranch || "")
        setIncomingBranch(data.incomingBranch || "")
      })
      .catch((error) => {
        if (error.name === 'AbortError') return
        console.log("Error fetching repository metadata", error)
      })

    fetch(`${API_BASE}/api/analysis`, { signal: controller.signal })
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load repo conflict analysis')
        }
        setAnalysis(payload.data || [])
      })
      .catch((err) => {
        if (err.name === 'AbortError') return
        console.error('Error fetching repository conflict analysis:', err)
        setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [])

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

  return (
    <div className="dashboard-page">
      <div className="welcome-section">
        <h1>Welcome back</h1>
        <p>Here are the AST diffs and conflicted files across your repo.</p>
      </div>

      <div className="repository-card">
        <div className="repository-icon">
          <i className="fa-regular fa-folder"></i>
        </div>

        <div className="repository-content">
          <h2>{repoName || 'Repository'}</h2>

          <div className="branch-flow">
            <div className="branch current-branch">
              <i className="fa-solid fa-code-branch"></i>
              <span>{currentBranch || 'unknown'}</span>
            </div>

            <i className="fa-solid fa-arrow-right branch-arrow"></i>

            <div className="branch main-branch">
              <i className="fa-solid fa-code-branch"></i>
              <span>{incomingBranch || 'incoming'}</span>
            </div>
          </div>

          <div className="repository-divider"></div>
          <div className="workspace-text">
            <i className="fa-solid fa-code"></i>
            <span>Your current merge workspace</span>
          </div>
        </div>
      </div>

      <div className="dashboard-summary">
        <div className="summary-card">
          <h3>Conflicted files</h3>
          <span>{analysis.length}</span>
        </div>
      </div>

      <div className="analysis-grid">
        {analysis.length === 0 ? (
          <div className="empty-state">
            <h3>No conflicted files found.</h3>
            <p>Run the serve command in a repository with active merge conflicts.</p>
          </div>
        ) : (
          analysis.map((item) => (
            <div className="analysis-card" key={item.file}>
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
                  <h4>AST Collision preview</h4>
                  {item.smartDiff.collisions.map((collision, index) => (
                    <div className="preview-row" key={`${collision.name}-${index}`}>
                      <strong>{collision.kind} {collision.name}</strong>
                      <span>{collision.type}</span>
                      <pre>{collision.base_content || collision.our_content || collision.their_content}</pre>
                    </div>
                  ))}
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

export default Dashboard;