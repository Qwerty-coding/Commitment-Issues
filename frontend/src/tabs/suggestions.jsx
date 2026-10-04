import { useCallback, useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import './suggestions.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

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
    case 'proposed':
      return 'Proposed'
    case 'previewed':
      return 'Previewed'
    case 'approved':
      return 'Approved'
    case 'applied':
      return 'Applied'
    case 'reverted':
      return 'Reverted'
    case 'stale':
      return 'Stale'
    case 'failed':
      return 'Failed'
    case 'validation_failed':
      return 'Validation Failed'
    case 'complete':
      return 'Complete'
    case 'below_threshold':
      return 'Below Threshold'
    case 'manual_review':
      return 'Manual Review'
    default:
      return status ? status.replace('_', ' ') : 'Proposed'
  }
}

function validationLabel(status) {
  switch (status) {
    case 'passed':
      return 'Passed'
    case 'failed':
      return 'Failed'
    case 'running':
      return 'Running...'
    case 'timed_out':
      return 'Timed Out'
    case 'cancelled':
      return 'Cancelled'
    case 'not_run':
    default:
      return 'Not Run'
  }
}

function Suggestions() {
  const location = useLocation()
  const query = new URLSearchParams(location.search)
  const file = query.get('file') || ''

  const [suggestions, setSuggestions] = useState([])
  const [resolutionsMap, setResolutionsMap] = useState({})
  const [meta, setMeta] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [reloadKey, setReloadKey] = useState(0)

  // Local action state per resolution ID: { [resId]: { inFlight, error, diffOpen, confirmRevert } }
  const [actionState, setActionState] = useState({})

  // Fetch suggestions and associated resolutions
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
      .then(async (payload) => {
        const items = payload.data || []
        setSuggestions(items)
        setMeta(payload.meta || null)
        setError(null)

        // Fetch corresponding resolutions list
        const resUrl = file
          ? `${API_BASE}/api/resolutions?file=${encodeURIComponent(file)}`
          : `${API_BASE}/api/resolutions`

        try {
          const resResponse = await fetch(resUrl, { signal: controller.signal })
          const resPayload = await resResponse.json()
          if (resResponse.ok && resPayload?.success && Array.isArray(resPayload.data)) {
            const map = {}
            resPayload.data.forEach((r) => {
              if (r.id) map[r.id] = r
            })
            setResolutionsMap(map)
          }
        } catch {
          // Non-fatal: individual cards can still query their resolution by ID
        }
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

  const setItemAction = (id, updates) => {
    setActionState((prev) => ({
      ...prev,
      [id]: { ...prev[id], ...updates },
    }))
  }

  // Handle Preview
  const handlePreview = async (item, resId) => {
    if (!resId) return
    setItemAction(resId, { inFlight: 'preview', error: null })
    try {
      const resp = await fetch(`${API_BASE}/api/resolutions/${resId}/preview`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          resolutionId: resId,
          repositoryRoot: item.repository,
          file: item.file,
          suggestionRevision: item.revision || 1,
        }),
      })
      const payload = await resp.json()
      if (!resp.ok || !payload?.success) {
        throw new Error(payload?.error?.message || payload?.error?.code || 'Preview failed')
      }
      setResolutionsMap((prev) => ({ ...prev, [resId]: payload.data }))
      setItemAction(resId, { inFlight: null, diffOpen: true, error: null })
    } catch (err) {
      setItemAction(resId, { inFlight: null, error: err.message })
    }
  }

  // Handle Approve
  const handleApprove = async (item, resId) => {
    if (!resId) return
    setItemAction(resId, { inFlight: 'approve', error: null })
    try {
      const resp = await fetch(`${API_BASE}/api/resolutions/${resId}/approve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          resolutionId: resId,
          repositoryRoot: item.repository,
          file: item.file,
          suggestionRevision: item.revision || 1,
        }),
      })
      const payload = await resp.json()
      if (!resp.ok || !payload?.success) {
        throw new Error(payload?.error?.message || payload?.error?.code || 'Approval failed')
      }
      setResolutionsMap((prev) => ({ ...prev, [resId]: payload.data }))
      setItemAction(resId, { inFlight: null, error: null })
    } catch (err) {
      setItemAction(resId, { inFlight: null, error: err.message })
    }
  }

  // Handle Apply
  const handleApply = async (item, resId) => {
    if (!resId) return
    setItemAction(resId, { inFlight: 'apply', error: null })
    try {
      const resp = await fetch(`${API_BASE}/api/resolutions/${resId}/apply`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          resolutionId: resId,
          repositoryRoot: item.repository,
          file: item.file,
          suggestionRevision: item.revision || 1,
        }),
      })
      const payload = await resp.json()
      if (!resp.ok || !payload?.success) {
        throw new Error(payload?.error?.message || payload?.error?.code || 'Apply failed')
      }
      setResolutionsMap((prev) => ({
        ...prev,
        [resId]: {
          ...payload.data,
          postApplyHash: payload.postApplyHash,
          validationStatus: payload.validationStatus || payload.data.validationStatus,
        },
      }))
      setItemAction(resId, { inFlight: null, error: null })
    } catch (err) {
      setItemAction(resId, { inFlight: null, error: err.message })
    }
  }

  // Handle Revert
  const handleRevert = async (item, resId, postApplyHash) => {
    if (!resId) return
    setItemAction(resId, { inFlight: 'revert', error: null, confirmRevert: false })
    try {
      const resp = await fetch(`${API_BASE}/api/resolutions/${resId}/revert`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          resolutionId: resId,
          repositoryRoot: item.repository,
          file: item.file,
          postApplyHash: postApplyHash || '',
        }),
      })
      const payload = await resp.json()
      if (!resp.ok || !payload?.success) {
        throw new Error(payload?.error?.message || payload?.error?.code || 'Revert failed')
      }
      setResolutionsMap((prev) => ({ ...prev, [resId]: payload.data }))
      setItemAction(resId, { inFlight: null, error: null })
    } catch (err) {
      setItemAction(resId, { inFlight: null, error: err.message })
    }
  }

  // Handle Refresh Analysis (after stale-file error)
  const handleRefreshAnalysis = async (item) => {
    try {
      await fetch(`${API_BASE}/api/analysis/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          repositoryRoot: item.repository,
          file: item.file,
        }),
      })
      retry()
    } catch (err) {
      console.error('Refresh analysis failed:', err)
    }
  }

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
        <h1>AI Suggestions & Patch Resolution</h1>
        <p>
          {file
            ? `Preview, approve, apply, and rollback safe patches for ${file}.`
            : 'Preview, approve, apply, and rollback safe patches for merge conflicts.'}
        </p>
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
          const resId = item.resolutionId || ''
          const resolution = resolutionsMap[resId] || null
          const itemActions = actionState[resId] || {}

          const currentStatus = resolution?.status || item.status || 'proposed'
          const approvalStatus = resolution?.approvalStatus || 'none'
          const validationStatus = resolution?.validationStatus || 'not_run'
          const isApproved = approvalStatus === 'approved'
          const isApplied = currentStatus === 'applied' || currentStatus === 'validation_failed'
          const isReverted = currentStatus === 'reverted'
          const isStale = currentStatus === 'stale'
          const isManualReview = currentStatus === 'manual_review' || item.status === 'manual_review'
          const inFlight = itemActions.inFlight || null
          const actionErr = itemActions.error || null
          const isStaleError = actionErr && (actionErr.includes('STALE_FILE') || actionErr.includes('stale'))

          return (
            <div
              className={`ai-suggestion-card ai-suggestion-card-${currentStatus}`}
              key={item.id || `${item.file}-${item.collisionKey || index}`}
            >
              <div className="ai-suggestion-title">
                <div className="suggestion-meta">
                  <div className="ai-suggestion-icon">AI</div>
                  <div>
                    <h3>{item.file}</h3>
                    <p>
                      {item.collision?.kind} {item.collision?.name} · Line {item.collision?.line}
                      {item.regionId ? ` · Region #${item.regionId}` : ''}
                      {item.revision ? ` · Rev ${item.revision}` : ''}
                    </p>
                    <p className="suggestion-provenance">
                      {item.provider}
                      {item.model ? ` · ${item.model}` : ''}
                      {item.repository ? ` · ${item.repository}` : ''}
                    </p>
                  </div>
                </div>

                <div className="suggestion-badges">
                  {/* Separate AI Confidence Score */}
                  <span className="ai-suggestion-score" title="AI Model Confidence Score">
                    AI Confidence: {confidence ?? 'N/A'}%
                  </span>

                  {/* Resolution Lifecycle Status */}
                  <span
                    className={`suggestion-status suggestion-status-${currentStatus}`}
                    title="Patch Lifecycle Status"
                  >
                    {statusLabel(currentStatus)}
                  </span>

                  {/* Separate Post-Apply Validation Status */}
                  {isApplied && (
                    <span
                      className={`validation-status-badge validation-status-${validationStatus}`}
                      title="Automated Test/Command Validation"
                    >
                      Validation: {validationLabel(validationStatus)}
                    </span>
                  )}
                </div>
              </div>

              {/* MANUAL REVIEW BANNER */}
              {isManualReview && (
                <div className="stale-warning-banner" style={{ borderColor: 'rgba(234, 179, 8, 0.4)', background: 'rgba(234, 179, 8, 0.1)' }}>
                  <div className="stale-warning-text">
                    <strong>Manual Review Required:</strong> {item.errorMessage || 'Conflict region cannot be mapped to the working tree or language is unsupported. Automatic patch apply is disabled.'}
                  </div>
                </div>
              )}

              {/* STALE FILE WARNING BANNER */}
              {(isStale || isStaleError) && (
                <div className="stale-warning-banner">
                  <div className="stale-warning-text">
                    <strong>Stale File Warning:</strong> The working-tree file or conflict region has changed since
                    analysis. Patches cannot be safely applied without fresh analysis.
                  </div>
                  <button
                    type="button"
                    className="action-btn refresh-analysis-btn"
                    onClick={() => handleRefreshAnalysis(item)}
                  >
                    Refresh Analysis
                  </button>
                </div>
              )}

              {/* ACTION ERROR DISPLAY */}
              {actionErr && !isStaleError && (
                <div className="resolution-error-banner">
                  <strong>Error:</strong> {actionErr}
                </div>
              )}

              <div className="ai-suggestion-content">
                {item.status === 'failed' ? (
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
                      <strong>AI Explanation</strong>
                      <p>{item.resolution?.explanation || 'No explanation provided.'}</p>
                    </div>

                    <div className="ai-suggestion-block">
                      <strong>Suggested Code Replacement</strong>
                      <pre>{item.resolution?.suggested_code || 'No suggested code available.'}</pre>
                    </div>

                    {/* READ-ONLY UNIFIED DIFF VIEW */}
                    {itemActions.diffOpen && (resolution?.previewDiff || resolution?.preview_diff) && (
                      <div className="ai-suggestion-block patch-diff-container">
                        <div className="diff-header">
                          <strong>Read-Only Patch Unified Diff</strong>
                          <span className="diff-note">(Safe in-memory preview — no disk mutation)</span>
                        </div>
                        <pre className="patch-diff-view">
                          {(resolution.previewDiff || resolution.preview_diff).split('\n').map((line, lIdx) => {
                            let lineClass = 'diff-line'
                            if (line.startsWith('+') && !line.startsWith('+++')) lineClass += ' diff-line-add'
                            else if (line.startsWith('-') && !line.startsWith('---')) lineClass += ' diff-line-del'
                            else if (line.startsWith('@@')) lineClass += ' diff-line-hunk'
                            return (
                              <div key={lIdx} className={lineClass}>
                                {line || ' '}
                              </div>
                            )
                          })}
                        </pre>
                      </div>
                    )}
                  </>
                )}
              </div>

              {/* ACTION TOOLBAR */}
              {item.status !== 'failed' && resId && (
                <div className="resolution-action-toolbar">
                  {/* PREVIEW BUTTON */}
                  <button
                    type="button"
                    className="action-btn preview-btn"
                    disabled={Boolean(inFlight) || isApplied || isStale || isManualReview}
                    onClick={() => {
                      if (resolution?.previewDiff && itemActions.diffOpen) {
                        setItemAction(resId, { diffOpen: false })
                      } else {
                        handlePreview(item, resId)
                      }
                    }}
                  >
                    {inFlight === 'preview'
                      ? 'Generating Preview...'
                      : itemActions.diffOpen && resolution?.previewDiff
                        ? 'Hide Diff'
                        : 'Preview Diff'}
                  </button>

                  {/* EXPLICIT APPROVE BUTTON */}
                  {!isApplied && !isReverted && (
                    <button
                      type="button"
                      className={`action-btn approve-btn ${isApproved ? 'approve-btn-approved' : ''}`}
                      disabled={Boolean(inFlight) || isApproved || isStale || isManualReview}
                      onClick={() => handleApprove(item, resId)}
                    >
                      {inFlight === 'approve'
                        ? 'Approving...'
                        : isApproved
                          ? (<><i className="fa-solid fa-check"></i> Explicitly Approved</>)
                          : 'Explicitly Approve'}
                    </button>
                  )}

                  {/* APPLY BUTTON (Disabled before approval!) */}
                  {!isApplied && !isReverted && (
                    <button
                      type="button"
                      className="action-btn apply-btn"
                      disabled={Boolean(inFlight) || !isApproved || isStale || isManualReview}
                      title={!isApproved ? 'Apply is disabled until explicit approval' : 'Apply patch atomically'}
                      onClick={() => handleApply(item, resId)}
                    >
                      {inFlight === 'apply' ? 'Applying Atomically...' : 'Apply Patch'}
                    </button>
                  )}

                  {/* REVERT BUTTON (Shown when applied) */}
                  {isApplied && !itemActions.confirmRevert && (
                    <button
                      type="button"
                      className="action-btn revert-btn"
                      disabled={Boolean(inFlight)}
                      onClick={() => setItemAction(resId, { confirmRevert: true })}
                    >
                      Revert Applied Patch
                    </button>
                  )}

                  {/* REVERT CONFIRMATION MODAL/INLINE */}
                  {isApplied && itemActions.confirmRevert && (
                    <div className="revert-confirm-inline">
                      <span className="revert-confirm-warning">
                        Confirm Rollback: This restores the exact pre-apply working tree content. Rollback will be
                        safely refused if subsequent edits were made to this file.
                      </span>
                      <button
                        type="button"
                        className="action-btn revert-confirm-btn"
                        disabled={Boolean(inFlight)}
                        onClick={() => handleRevert(item, resId, resolution?.postApplyHash)}
                      >
                        {inFlight === 'revert' ? 'Reverting...' : 'Confirm Rollback'}
                      </button>
                      <button
                        type="button"
                        className="action-btn revert-cancel-btn"
                        disabled={Boolean(inFlight)}
                        onClick={() => setItemAction(resId, { confirmRevert: false })}
                      >
                        Cancel
                      </button>
                    </div>
                  )}

                  {/* REVERTED BADGE */}
                  {isReverted && <span className="reverted-notice"><i className="fa-solid fa-check"></i> Patch was safely rolled back to original content.</span>}
                </div>
              )}
            </div>
          )
        })
      )}
    </div>
  )
}

export default Suggestions
