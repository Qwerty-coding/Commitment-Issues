import { useCallback, useEffect, useState } from 'react'
import {
  Clock,
  RefreshCw,
  AlertCircle,
  CheckCircle2,
  AlertTriangle,
  FolderGit2,
  Cpu,
  Activity,
} from 'lucide-react'
import './history.css'

import { API_BASE, fetchJson, isAbort } from '../lib/api'

function formatTimestamp(value) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString()
}

function runStatus(run) {
  if (run.cancelled) return { label: 'Cancelled', tone: 'warning', icon: AlertTriangle }
  if (run.timedOut) return { label: 'Timed out', tone: 'warning', icon: AlertTriangle }
  if (!run.completedAt) return { label: 'In progress', tone: 'pending', icon: Clock }
  return { label: 'Completed', tone: 'ok', icon: CheckCircle2 }
}

function History() {
  const [runs, setRuns] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    fetchJson(`${API_BASE}/api/history`, { signal: controller.signal })
      .then((payload) => {
        setRuns(payload.data || [])
        setError('')
      })
      .catch((err) => {
        if (isAbort(err)) return
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
        <div className="history-empty">
          <Clock size={20} style={{ display: 'inline-block', verticalAlign: 'middle', marginRight: 8, color: '#6366f1' }} />
          <span>Loading run history...</span>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="history-page">
        <h1>Run History</h1>
        <div className="history-empty history-error">
          <AlertCircle size={24} style={{ color: '#ef4444', marginBottom: 8 }} />
          <p>Error loading history: {error}</p>
          <button type="button" className="history-retry" onClick={retry}>
            <RefreshCw size={14} style={{ marginRight: 6, verticalAlign: 'middle' }} />
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
          <RefreshCw size={14} style={{ marginRight: 6, verticalAlign: 'middle' }} />
          Refresh
        </button>
      </div>
      <p className="history-subtitle">
        Completed and in-progress analysis runs, with the provider/model used and resolution outcomes.
      </p>

      {runs.length === 0 ? (
        <div className="history-empty">
          <Activity size={24} style={{ color: '#94a3b8', marginBottom: 8 }} />
          <div>No runs recorded yet. Start the server with a conflicted repository to populate history.</div>
        </div>
      ) : (
        <div className="history-grid">
          {runs.map((run) => {
            const status = runStatus(run)
            const StatusIcon = status.icon
            return (
              <div className="history-card" key={`${run.runId}-${run.repository}`}>
                <div className="history-card-head">
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <FolderGit2 size={18} style={{ color: '#4f46e5' }} />
                    <h3>{run.repository}</h3>
                  </div>
                  <span className={`history-status history-status-${status.tone}`}>
                    {StatusIcon && <StatusIcon size={12} style={{ marginRight: 4, verticalAlign: 'middle' }} />}
                    {status.label}
                  </span>
                </div>

                <div className="history-provider">
                  <Cpu size={14} style={{ marginRight: 6, verticalAlign: 'middle' }} />
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
                      <li key={`${summary.slice(0, 24)}-${index}`}>{summary}</li>
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
