import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import './suggestions.css'

const API_BASE = import.meta.env.VITE_API_BASE || 'http://localhost:8080'

function Suggestions() {
  const location = useLocation()
  const query = new URLSearchParams(location.search)
  const file = query.get('file') || ''

  const [suggestions, setSuggestions] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const url = file ? `${API_BASE}/api/suggestions?file=${encodeURIComponent(file)}` : `${API_BASE}/api/suggestions`
    fetch(url)
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load suggestions')
        }
        setSuggestions(payload.data || [])
      })
      .catch((err) => {
        console.error('Error fetching suggestions:', err)
        setError(err.message)
      })
      .finally(() => setLoading(false))
  }, [file])

  if (loading) {
    return <h2>Loading suggestions...</h2>
  }

  if (error) {
    return <h2>Error loading suggestions: {error}</h2>
  }

  return (
    <div className="suggestions-page">
      <div className="suggestions-header">
        <h1>AI Suggestions</h1>
        <p>{file ? `AI suggestions for ${file}` : 'AI-generated resolution suggestions for current merge conflicts.'}</p>
      </div>

      {suggestions.length === 0 ? (
        <div className="empty-state">No suggestions available yet.</div>
      ) : (
        suggestions.map((item, index) => (
          <div className="ai-suggestion-card" key={`${item.file}-${index}`}>
            <div className="ai-suggestion-title">
              <div className="suggestion-meta">
                <div className="ai-suggestion-icon">AI</div>
                <div>
                  <h3>{item.file}</h3>
                  <p>{item.collision?.kind} {item.collision?.name} · Line {item.collision?.line}</p>
                </div>
              </div>
              <div className="ai-suggestion-score">
                {item.resolution?.confidence_score ?? item.resolution?.confidence ?? 'N/A'}%
              </div>
            </div>

            <div className="ai-suggestion-content">
              <div className="ai-suggestion-block">
                <strong>Explanation</strong>
                <p>{item.resolution?.explanation || 'No explanation provided.'}</p>
              </div>
              <div className="ai-suggestion-block">
                <strong>Suggested Code</strong>
                <pre>{item.resolution?.suggested_code || 'No suggested code available.'}</pre>
              </div>
            </div>
          </div>
        ))
      )}
    </div>
  )
}

export default Suggestions;
