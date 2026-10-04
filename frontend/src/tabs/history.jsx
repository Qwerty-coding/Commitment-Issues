import { useCallback, useEffect, useState } from 'react'
import './history.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function formatTimestamp(value) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString()
}

function runStatus(run) {
  if (run.cancelled) return { label: 'Cancelled', tone: 'warning' }
  if (run.timedOut) return { label: 'Timed out', tone: 'warning' }
  if (!run.completedAt) return { label: 'In progress', tone: 'pending' }
  return { label: 'Completed', tone: 'ok' }
}

function History() {
  const [runs, setRuns] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    fetch(`${API_BASE}/api/history`, { signal: controller.signal })
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load history')
        }
        setRuns(payload.data || [])
        setError('')
      })
      .catch((err) => {
        if (err.name === 'AbortError') return
        console.error('Error loading history:', err)
        setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [reloadKey])

  const retry = useCallback(() => {
    setLoading(true)
    setError('')
    setReloadKey((key) => key + 1)
  }, [])

  if (loading) {
    return (
      <div className="history-page">
        <h1>Run History</h1>
        <div className="history-empty">Loading run history...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="history-page">
        <h1>Run History</h1>
        <div className="history-empty history-error">
          <p>Error loading history: {error}</p>
          <button type="button" className="history-retry" onClick={retry}>
            Retry
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="history-page">
      <div className="history-header">
        <h1>Run History</h1>
        <button type="button" className="history-refresh" onClick={retry}>
          Refresh
        </button>
      </div>
      <p className="history-subtitle">
        Completed and in-progress analysis runs, with the provider/model used and resolution outcomes.
      </p>

      {runs.length === 0 ? (
        <div className="history-empty">
          No runs recorded yet. Start the server with a conflicted repository to populate history.
        </div>
      ) : (
        <div className="history-grid">
          {runs.map((run) => {
            const status = runStatus(run)
            return (
              <div className="history-card" key={`${run.runId}-${run.repository}`}>
                <div className="history-card-head">
                  <h3>{run.repository}</h3>
                  <span className={`history-status history-status-${status.tone}`}>{status.label}</span>
                </div>

                <div className="history-provider">
                  {run.provider}
                  {run.model ? ` · ${run.model}` : ''}
                </div>

                <div className="metric">
                  <span>Run ID</span>
                  <strong>{run.runId}</strong>
                </div>
                <div className="metric">
                  <span>Started</span>
                  <strong>{formatTimestamp(run.startedAt)}</strong>
                </div>
                <div className="metric">
                  <span>Completed</span>
                  <strong>{formatTimestamp(run.completedAt)}</strong>
                </div>
                <div className="metric">
                  <span>Files analyzed</span>
                  <strong>{run.filesAnalyzed ?? 0}</strong>
                </div>
                <div className="metric">
                  <span>Collisions</span>
                  <strong>{run.collisionCount ?? 0}</strong>
                </div>
                <div className="metric">
                  <span>Successful suggestions</span>
                  <strong>{run.successfulSuggestions ?? 0}</strong>
                </div>
                <div className="metric">
                  <span>Failed suggestions</span>
                  <strong className={(run.failedSuggestions ?? 0) > 0 ? 'history-value-error' : ''}>
                    {run.failedSuggestions ?? 0}
                  </strong>
                </div>
                {(run.belowThresholdSuggestions ?? 0) > 0 && (
                  <div className="metric">
                    <span>Below threshold</span>
                    <strong>{run.belowThresholdSuggestions}</strong>
                  </div>
                )}

                {(run.errorSummaries || []).length > 0 && (
                  <ul className="history-errors">
                    {run.errorSummaries.map((summary, index) => (
                      <li key={index}>{summary}</li>
                    ))}
                  </ul>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

export default History
