import { useEffect, useState } from 'react'
import './history.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function History() {
  const [stats, setStats] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    fetch(`${API_BASE}/api/prompt/statistics`)
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load history')
        }
        setStats(payload.data || [])
      })
      .catch((err) => {
        console.error('Error loading history:', err)
        setError(err.message)
      })
      .finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <h2>Loading history...</h2>
  }

  if (error) {
    return <h2>Error loading history: {error}</h2>
  }

  return (
    <div className="history-page">
      <h1>Prompt History</h1>
      {stats.length === 0 ? (
        <div className="history-empty">No prompt history available.</div>
      ) : (
        <div className="history-grid">
          {stats.map((item) => (
            <div className="history-card" key={item.file}>
              <h3>{item.file}</h3>
              <p>Learn how prompt selection changed the analysis footprint for this file.</p>
              <div className="metric">
                <span>Selected Nodes</span>
                <strong>{item.selectedNodes}</strong>
              </div>
              <div className="metric">
                <span>Excluded Nodes</span>
                <strong>{item.excludedNodes}</strong>
              </div>
              <div className="metric">
                <span>Estimated Tokens</span>
                <strong>{item.estimatedTokens}</strong>
              </div>
              <div className="metric">
                <span>Reduction</span>
                <strong>{item.reductionPercentage.toFixed(1)}%</strong>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default History
;