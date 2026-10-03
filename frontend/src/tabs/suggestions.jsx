import { useCallback, useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import './suggestions.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

// Provider/setup failures surface as a distinct, actionable state instead of a
// generic error, mirroring the typed codes the API returns.
const PROVIDER_ERROR_CODES = new Set([
  'PROVIDER_UNAVAILABLE',
  'MODEL_MISSING',
  'API_KEY_MISSING',
  'AI_CONFIG_INVALID',
  'BASE_URL_INVALID',
  'PROVIDER_UNSUPPORTED',
])

function statusLabel(status) {
  switch (status) {
    case 'complete':
      return 'Complete'
    case 'below_threshold':
      return 'Below threshold'
    case 'failed':
      return 'Failed'
    default:
      return status || 'Unknown'
  }
}

function Suggestions() {
  const location = useLocation()
  const query = new URLSearchParams(location.search)
  const file = query.get('file') || ''

  const [suggestions, setSuggestions] = useState([])
  const [meta, setMeta] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    const url = file
      ? `${API_BASE}/api/suggestions?file=${encodeURIComponent(file)}`
      : `${API_BASE}/api/suggestions`

    fetch(url, { signal: controller.signal })
      .then(async (response) => {
        const payload = await response.json().catch(() => null)
        if (!response.ok || !payload?.success) {
          const failure = new Error(payload?.error?.message || `Request failed (${response.status})`)
          failure.code = payload?.error?.code || (response.status === 504 ? 'TIMEOUT' : '')
          throw failure
        }
        return payload
      })
      .then((payload) => {
        setSuggestions(payload.data || [])
        setMeta(payload.meta || null)
        setError(null)
      })
      .catch((err) => {
        if (err.name === 'AbortError') return
        console.error('Error fetching suggestions:', err)
        setError({ message: err.message, code: err.code || '' })
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [file, reloadKey])

  const retry = useCallback(() => {
    setLoading(true)
    setError(null)
    setReloadKey((key) => key + 1)
  }, [])

  const hasPartialFailure = Boolean(meta && meta.failed > 0)
  const canRetry = Boolean(meta && (meta.retryable || hasPartialFailure))
  const providerUnavailable = Boolean(error && PROVIDER_ERROR_CODES.has(error.code))

  if (loading) {
    return (
      <div className="suggestions-page">
        <div className="suggestions-header">
          <h1>AI Suggestions</h1>
          <p>Generating resolution suggestions...</p>
        </div>
        <div className="suggestions-state">Loading suggestions...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="suggestions-page">
        <div className="suggestions-header">
          <h1>AI Suggestions</h1>
          <p>{file ? `AI suggestions for ${file}` : 'AI-generated resolution suggestions for current merge conflicts.'}</p>
        </div>
        {providerUnavailable ? (
          <div className="suggestions-state suggestions-state-warning">
            <h2>AI provider unavailable</h2>
            <p>
              The configured provider could not be reached or is not ready. Start Ollama and make sure the
              configured model is installed, then retry.
            </p>
            <p className="suggestions-state-detail">
              {error.code ? `${error.code}: ` : ''}
              {error.message}
            </p>
            <button type="button" className="suggestions-retry" onClick={retry}>
              Retry
            </button>
          </div>
        ) : error.code === 'TIMEOUT' ? (
          <div className="suggestions-state suggestions-state-warning">
            <h2>Request timed out</h2>
            <p>The generation request exceeded its deadline before completing.</p>
            <p className="suggestions-state-detail">{error.message}</p>
            <button type="button" className="suggestions-retry" onClick={retry}>
              Retry
            </button>
          </div>
        ) : (
          <div className="suggestions-state suggestions-state-error">
            <h2>Error loading suggestions</h2>
            <p>{error.message}</p>
            <button type="button" className="suggestions-retry" onClick={retry}>
              Retry
            </button>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="suggestions-page">
      <div className="suggestions-header">
        <h1>AI Suggestions</h1>
        <p>{file ? `AI suggestions for ${file}` : 'AI-generated resolution suggestions for current merge conflicts.'}</p>
        {meta && (
          <div className="suggestions-meta">
            <span className="suggestions-chip">
              {meta.provider}
              {meta.model ? ` · ${meta.model}` : ''}
            </span>
            {meta.runId && <span className="suggestions-chip">run {meta.runId}</span>}
            <span className="suggestions-chip">{meta.generated} generated</span>
            {meta.reused > 0 && <span className="suggestions-chip">{meta.reused} reused</span>}
            {meta.belowThreshold > 0 && (
              <span className="suggestions-chip suggestions-chip-warning">{meta.belowThreshold} below threshold</span>
            )}
            {meta.failed > 0 && (
              <span className="suggestions-chip suggestions-chip-error">{meta.failed} failed</span>
            )}
          </div>
        )}
      </div>

      {hasPartialFailure && (
        <div className="suggestions-partial">
          <div>
            <strong>Some collisions could not be resolved.</strong>
            <p>Successful suggestions are shown below; only the failures will be regenerated on retry.</p>
            <ul className="suggestions-failures">
              {(meta.failures || []).map((failure, index) => (
                <li key={`${failure.file}-${failure.collisionKey}-${index}`}>
                  <code>{failure.file}</code> · {failure.collisionKey} — {failure.code}
                  {failure.retryable ? ' (retryable)' : ''}
                </li>
              ))}
            </ul>
          </div>
          {canRetry && (
            <button type="button" className="suggestions-retry" onClick={retry}>
              Retry failures
            </button>
          )}
        </div>
      )}

      {suggestions.length === 0 ? (
        <div className="empty-state">No suggestions available yet.</div>
      ) : (
        suggestions.map((item, index) => {
          const confidence = item.resolution?.confidence_score ?? item.resolution?.confidence
          const status = item.status || 'complete'
          return (
            <div
              className={`ai-suggestion-card ai-suggestion-card-${status}`}
              key={item.id || `${item.file}-${item.collisionKey || index}`}
            >
              <div className="ai-suggestion-title">
                <div className="suggestion-meta">
                  <div className="ai-suggestion-icon">AI</div>
                  <div>
                    <h3>{item.file}</h3>
                    <p>
                      {item.collision?.kind} {item.collision?.name} · Line {item.collision?.line}
                    </p>
                    <p className="suggestion-provenance">
                      {item.provider}
                      {item.model ? ` · ${item.model}` : ''}
                      {item.repository ? ` · ${item.repository}` : ''}
                    </p>
                  </div>
                </div>
                <div className="suggestion-badges">
                  <span className={`suggestion-status suggestion-status-${status}`}>{statusLabel(status)}</span>
                  <span className="ai-suggestion-score">{confidence ?? 'N/A'}%</span>
                </div>
              </div>

              <div className="ai-suggestion-content">
                {status === 'failed' ? (
                  <div className="ai-suggestion-block">
                    <strong>Error</strong>
                    <p>
                      {item.errorCode ? `${item.errorCode}: ` : ''}
                      {item.errorMessage || 'Resolution failed.'}
                      {item.retryable ? ' This failure is retryable.' : ''}
                    </p>
                  </div>
                ) : (
                  <>
                    <div className="ai-suggestion-block">
                      <strong>Explanation</strong>
                      <p>{item.resolution?.explanation || 'No explanation provided.'}</p>
                    </div>
                    <div className="ai-suggestion-block">
                      <strong>Suggested Code</strong>
                      <pre>{item.resolution?.suggested_code || 'No suggested code available.'}</pre>
                    </div>
                  </>
                )}
              </div>
            </div>
          )
        })
      )}
    </div>
  )
}

export default Suggestions
