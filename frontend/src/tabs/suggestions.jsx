import { useCallback, useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import {
  Sparkles,
  Check,
  CheckCircle2,
  AlertTriangle,
  RotateCcw,
  Eye,
  RefreshCw,
  FileCode,
  ShieldCheck,
} from 'lucide-react'
import './suggestions.css'

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
      return 'Unverified AI Score'
    case 'manual_review':
      return 'Manual Review'
    default:
      return status ? status.split('_').join(' ') : 'Proposed'
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
  const [globalError, setGlobalError] = useState(null)

  // Local action state per resolution ID: { [resId]: { inFlight, error, diffOpen, confirmRevert } }
  const [actionState, setActionState] = useState({})

  // Fetch suggestions and associated resolutions
  useEffect(() => {
    const controller = new AbortController()

    const url = file
      ? `${API_BASE}/api/suggestions?file=${encodeURIComponent(file)}`
      : `${API_BASE}/api/suggestions`

    fetchJson(url, { signal: controller.signal })
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
          const resPayload = await fetchJson(resUrl, { signal: controller.signal })
          if (Array.isArray(resPayload.data)) {
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
        if (isAbort(err)) return
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
    setGlobalError(null)
    setActionState({})
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
      setResolutionsMap((prev) => ({ ...prev, [resId]: payload.data.resolution }))
      setItemAction(resId, { inFlight: null, error: null })
    } catch (err) {
      setItemAction(resId, { inFlight: null, error: err.message })
    }
  }

  // Handle Revert
  const handleRevert = async (item, resId, postApplyHash) => {
    if (!resId) return
    setItemAction(resId, { inFlight: 'revert', error: null })
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
      setItemAction(resId, { inFlight: null, confirmRevert: false, error: null })
    } catch (err) {
      setItemAction(resId, { inFlight: null, error: err.message })
    }
  }

  // Handle Refresh Analysis (after stale-file error)
  const handleRefreshAnalysis = async (item) => {
    setGlobalError(null)
    try {
      await fetchJson(`${API_BASE}/api/analysis/refresh`, {
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
      setGlobalError({ message: err.message, code: err.code || '' })
    }
  }

  const hasPartialFailure = Boolean(meta && meta.failed > 0)
  const canRetry = Boolean(meta && (meta.retryable || hasPartialFailure))
  const providerUnavailable = Boolean(error && PROVIDER_ERROR_CODES.has(error.code))

  if (loading) {
    return (
      <div className="suggestions-page">
        <div className="suggestions-header">
          <h1>Resolution Suggestions</h1>
          <p>Generating AST-driven resolution suggestions...</p>
        </div>
        <div className="suggestions-state">
          <RefreshCw size={24} className="spin-icon" />
          <span>Analyzing conflicts and generating suggestions...</span>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="suggestions-page">
        <div className="suggestions-header">
          <h1>Resolution Suggestions</h1>
          <p>{file ? `Suggestions for ${file}` : 'Resolution suggestions for active merge collisions.'}</p>
        </div>
        {providerUnavailable ? (
          <div className="suggestions-state suggestions-state-warning">
            <h2>AI Provider Unavailable</h2>
            <p>
              The configured AI provider could not be reached. Ensure your local provider (e.g. Ollama) is running
              and the model is downloaded, or configure API credentials.
            </p>
            <p className="suggestions-state-detail">
              {error.code ? `${error.code}: ` : ''}
              {error.message}
            </p>
            <button type="button" className="suggestions-retry" onClick={retry}>
              Retry Connection
            </button>
          </div>
        ) : error.code === 'TIMEOUT' ? (
          <div className="suggestions-state suggestions-state-warning">
            <h2>Request Timed Out</h2>
            <p>The suggestion request exceeded the configured deadline.</p>
            <p className="suggestions-state-detail">{error.message}</p>
            <button type="button" className="suggestions-retry" onClick={retry}>
              Retry
            </button>
          </div>
        ) : (
          <div className="suggestions-state suggestions-state-error">
            <h2>Error Loading Suggestions</h2>
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
        <h1>Resolution Suggestions & Patch Safety</h1>
        <p>
          {file
            ? `Review, preview diff, approve, and safely apply patches for ${file}.`
            : 'Review, preview diff, approve, and safely apply patches for merge collisions.'}
        </p>
        {globalError && (
          <div className="suggestions-state suggestions-state-error" role="alert" aria-live="assertive">
            <p><strong>Refresh analysis failed:</strong> {globalError.message}</p>
            <button type="button" className="suggestions-retry" onClick={() => setGlobalError(null)}>
              Dismiss
            </button>
          </div>
        )}
        {meta && (
          <div className="suggestions-meta">
            <span className="suggestions-chip">
              {meta.provider}
              {meta.model ? ` · ${meta.model}` : ''}
            </span>
            {meta.runId && <span className="suggestions-chip">run {meta.runId}</span>}
            <span className="suggestions-chip">{meta.generated} generated</span>
            {meta.reused > 0 && <span className="suggestions-chip">{meta.reused} reused</span>}
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
            <p>Successful suggestions are shown below; failed ones can be retried.</p>
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
              Retry Failures
            </button>
          )}
        </div>
      )}

      {suggestions.length === 0 ? (
        <div className="empty-state">No conflict suggestions available for this repository.</div>
      ) : (
        suggestions.map((item, index) => {
          const confidence = item.resolution?.confidence_score ?? item.resolution?.confidence
          const verification = item.verification || item.resolution?.verification || resolutionsMap[item.resolutionId]?.verification
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
                  <div className="ai-suggestion-icon">
                    <Sparkles size={20} />
                  </div>
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
                  {/* Objective Verification Badge */}
                  {verification ? (
                    <span
                      className={`verification-badge ${verification.score < 70 ? 'verification-badge-warning' : ''}`}
                      title={`Deterministic verification score: ${verification.score}/100 (${verification.status || 'evaluated'})`}
                    >
                      <ShieldCheck size={13} />
                      Verified: {verification.score}/100
                    </span>
                  ) : (
                    <span className="verification-badge" title="Deterministic AST syntax check passes without error nodes">
                      <ShieldCheck size={13} />
                      Syntax Checked
                    </span>
                  )}

                  {/* AI self-score: displayed as informational metadata only */}
                  {confidence !== undefined && confidence !== null && (
                    <span className="ai-suggestion-score" title="Self-reported model score (uncalibrated post-hoc value)">
                      Model Score: {confidence}%
                    </span>
                  )}

                  {/* Lifecycle Status */}
                  <span
                    className={`suggestion-status suggestion-status-${currentStatus}`}
                    title="Patch Lifecycle Status"
                  >
                    {statusLabel(currentStatus)}
                  </span>

                  {/* Post-Apply Validation */}
                  {isApplied && (
                    <span
                      className={`validation-status-badge validation-status-${validationStatus}`}
                      title="Post-Apply Automated Validation"
                    >
                      {validationStatus === 'passed' ? <CheckCircle2 size={13} /> : <AlertTriangle size={13} />}
                      Validation: {validationLabel(validationStatus)}
                    </span>
                  )}
                </div>
              </div>

              {/* MANUAL REVIEW BANNER */}
              {isManualReview && (
                <div className="stale-warning-banner" style={{ borderColor: 'rgba(234, 179, 8, 0.4)', background: 'rgba(234, 179, 8, 0.1)' }}>
                  <div className="stale-warning-text">
                    <AlertTriangle size={18} style={{ color: '#eab308', flexShrink: 0 }} />
                    <div>
                      <strong>Manual Review Required:</strong> {item.errorMessage || 'Conflict region cannot be safely mapped or language is unsupported. Automatic patch apply is disabled to prevent corruption.'}
                    </div>
                  </div>
                </div>
              )}

              {/* STALE FILE WARNING BANNER */}
              {(isStale || isStaleError) && (
                <div className="stale-warning-banner">
                  <div className="stale-warning-text">
                    <AlertTriangle size={18} style={{ color: '#ef4444', flexShrink: 0 }} />
                    <div>
                      <strong>Stale File Warning:</strong> The working-tree file or conflict region has changed since
                      analysis. Patches cannot be safely applied without fresh analysis.
                    </div>
                  </div>
                  <button
                    type="button"
                    className="action-btn refresh-analysis-btn"
                    onClick={() => handleRefreshAnalysis(item)}
                  >
                    <RefreshCw size={14} /> Refresh Analysis
                  </button>
                </div>
              )}

              {/* ACTION IN-FLIGHT OR ERROR FEEDBACK */}
              {actionErr && !isStaleError && (
                <div className="suggestion-action-error">
                  <AlertTriangle size={14} />
                  <span><strong>Action Failed:</strong> {actionErr}</span>
                </div>
              )}

              <div className="ai-suggestion-body">
                {item.status === 'failed' ? (
                  <div className="suggestion-failed-box">
                    <strong>Suggestion generation failed for this collision:</strong>
                    <p className="suggestion-failed-message">
                      <code>{item.errorCode || 'UNKNOWN'}</code>: {item.errorMessage || 'Internal provider failure'}
                      {item.retryable ? ' This failure is retryable.' : ''}
                    </p>
                  </div>
                ) : (
                  <>
                    <div className="ai-suggestion-block">
                      <strong>AI Explanation & Strategy</strong>
                      <p>{item.resolution?.explanation || 'No explanation provided.'}</p>
                    </div>

                    <div className="ai-suggestion-block">
                      <strong>Proposed Resolution Code</strong>
                      <pre>{item.resolution?.suggested_code || 'No suggested code available.'}</pre>
                    </div>

                    {verification?.findings?.length > 0 && (
                      <div className="verification-findings-box">
                        <strong>Deterministic Verification Findings (-{100 - verification.score} pts):</strong>
                        <ul>
                          {verification.findings.map((f, fIdx) => (
                            <li key={fIdx}>
                              <span className="finding-check">{f.check}</span> (-{f.weight} pts): {f.details}
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}

                    {/* READ-ONLY UNIFIED DIFF VIEW */}
                    {itemActions.diffOpen && (resolution?.previewDiff || resolution?.preview_diff) && (
                      <div className="ai-suggestion-block patch-diff-container">
                        <div className="diff-header">
                          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                            <FileCode size={16} />
                            <strong>Read-Only Unified Patch Diff</strong>
                          </div>
                          <span className="diff-note">(Safe in-memory preview — no disk mutation)</span>
                        </div>
                        <div className="patch-diff-view" role="table" aria-label="Patch unified diff">
                          {(resolution.previewDiff || resolution.preview_diff).split('\n').map((line, lIdx) => {
                            let lineClass = 'diff-line'
                            if (line.startsWith('+') && !line.startsWith('+++')) lineClass += ' diff-line-add'
                            else if (line.startsWith('-') && !line.startsWith('---')) lineClass += ' diff-line-del'
                            else if (line.startsWith('@@')) lineClass += ' diff-line-hunk'
                            return (
                              <div key={lIdx} className={lineClass} role="row">
                                <span role="cell">{line || ' '}</span>
                              </div>
                            )
                          })}
                        </div>
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
                    <Eye size={15} />
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
                      <Check size={15} />
                      {inFlight === 'approve'
                        ? 'Approving...'
                        : isApproved
                          ? 'Explicitly Approved'
                          : 'Explicitly Approve'}
                    </button>
                  )}

                  {/* APPLY BUTTON (Disabled before approval) */}
                  {!isApplied && !isReverted && (
                    <button
                      type="button"
                      className="action-btn apply-btn"
                      disabled={Boolean(inFlight) || !isApproved || isStale || isManualReview}
                      title={!isApproved ? 'Apply is disabled until explicit approval' : 'Apply patch atomically'}
                      onClick={() => handleApply(item, resId)}
                    >
                      <ShieldCheck size={15} />
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
                      <RotateCcw size={15} /> Revert Applied Patch
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
                  {isReverted && (
                    <span className="reverted-notice">
                      <Check size={14} /> Patch was safely rolled back to original content.
                    </span>
                  )}
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
