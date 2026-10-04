import { useEffect, useRef, useState } from 'react'
import './compare.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

const STATUS_LABELS = {
  ours_only: 'Ours only',
  theirs_only: 'Theirs only',
  identical: 'Identical',
  structural_collision: 'Structural collision',
  content_conflict: 'Content conflict',
  add_add_conflict: 'Add/Add conflict',
  delete_modify_conflict: 'Delete/Modify conflict',
  rename_conflict: 'Rename conflict',
  binary_conflict: 'Binary conflict',
  unsupported_language: 'Unsupported language',
  analysis_error: 'Analysis error',
}

function isConflict(status) {
  return status && status !== 'ours_only' && status !== 'theirs_only' && status !== 'identical'
}

function SummaryCard({ label, value, tone }) {
  return (
    <div className={`compare-summary-card${tone ? ` compare-summary-card-${tone}` : ''}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function Compare() {
  const [repo, setRepo] = useState('.')
  const [base, setBase] = useState('')
  const [ours, setOurs] = useState('')
  const [theirs, setTheirs] = useState('')

  const [result, setResult] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  // Abort any in-flight comparison when the tab unmounts.
  const abortRef = useRef(null)
  useEffect(() => () => abortRef.current?.abort(), [])

  const runCompare = async (event) => {
    event.preventDefault()
    if (!ours.trim() || !theirs.trim()) {
      setError({ message: 'Both "ours" and "theirs" commit hashes or refs are required.' })
      return
    }

    setLoading(true)
    setError(null)
    setResult(null)

    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    try {
      const response = await fetch(`${API_BASE}/api/compare`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify({
          repositoryRoot: repo.trim() || '.',
          baseRef: base.trim(),
          oursRef: ours.trim(),
          theirsRef: theirs.trim(),
        }),
      })
      const payload = await response.json().catch(() => null)
      if (!response.ok || !payload?.success) {
        const failure = new Error(payload?.error?.message || `Comparison failed (${response.status})`)
        failure.code = payload?.error?.code || ''
        throw failure
      }
      setResult(payload.data)
    } catch (err) {
      if (err.name === 'AbortError') return
      setError({ message: err.message, code: err.code || '' })
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }

  return (
    <div className="compare-page">
      <h1>Compare Commits</h1>
      <p className="compare-subtitle">
        Compare two commits or branches without touching the working tree. The merge base is
        auto-detected and AST-level structural collisions are reported per file.
      </p>

      <form className="compare-form" onSubmit={runCompare}>
        <label className="compare-field">
          <span>Repository path</span>
          <input value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="." />
        </label>
        <label className="compare-field">
          <span>Base (optional)</span>
          <input value={base} onChange={(e) => setBase(e.target.value)} placeholder="auto-detected merge base" />
        </label>
        <label className="compare-field">
          <span>Ours</span>
          <input value={ours} onChange={(e) => setOurs(e.target.value)} placeholder="commit hash or branch" />
        </label>
        <label className="compare-field">
          <span>Theirs</span>
          <input value={theirs} onChange={(e) => setTheirs(e.target.value)} placeholder="commit hash or branch" />
        </label>
        <button type="submit" className="compare-submit" disabled={loading}>
          {loading ? 'Comparing…' : 'Compare'}
        </button>
      </form>

      {error && (
        <div className="compare-error">
          <strong>Comparison error</strong>
          <p>{error.code ? `${error.code}: ` : ''}{error.message}</p>
        </div>
      )}

      {loading && <div className="compare-state">Running AST comparison…</div>}

      {result && (
        <>
          <div className="compare-meta">
            <div>
              <h2>{result.repo1}{result.repo2 ? ` ↔ ${result.repo2}` : ''}</h2>
              <p>
                {result.unrelatedRepositories
                  ? 'Unrelated repositories — full tree comparison (no common merge base).'
                  : `Merge base: ${result.baseRef || 'n/a'}`}
              </p>
            </div>
            <div className="compare-refs">
              <span>Ours: {result.oursRef}</span>
              <span>Theirs: {result.theirsRef}</span>
            </div>
          </div>

          <div className="compare-summary">
            <SummaryCard label="Total files" value={result.summary?.totalFiles ?? 0} />
            <SummaryCard label="Structural collisions" value={result.summary?.structuralCollisions ?? 0} tone="conflict" />
            <SummaryCard label="Content conflicts" value={result.summary?.contentConflicts ?? 0} tone="conflict" />
            <SummaryCard label="Add/Add" value={result.summary?.addAddConflicts ?? 0} tone="conflict" />
            <SummaryCard label="Delete/Modify" value={result.summary?.deleteModifyConflicts ?? 0} tone="conflict" />
            <SummaryCard label="Rename" value={result.summary?.renameConflicts ?? 0} tone="conflict" />
            <SummaryCard label="Binary" value={result.summary?.binaryConflicts ?? 0} tone="conflict" />
            <SummaryCard label="Ours only" value={result.summary?.oursOnly ?? 0} />
            <SummaryCard label="Theirs only" value={result.summary?.theirsOnly ?? 0} />
            <SummaryCard label="Identical" value={result.summary?.identical ?? 0} />
            <SummaryCard label="Unsupported" value={result.summary?.unsupported ?? 0} />
            <SummaryCard label="Errors" value={result.summary?.errors ?? 0} tone="error" />
          </div>

          {(!result.files || result.files.length === 0) ? (
            <div className="compare-state">No changed files between the compared refs.</div>
          ) : (
            <div className="compare-file-list">
              {result.files.map((file) => {
                const conflict = isConflict(file.status)
                const collisions = file.smartDiff?.collisions || []
                return (
                  <div
                    className={`compare-file${conflict ? ' compare-file-conflict' : ''}`}
                    key={`${file.path}-${file.status}`}
                  >
                    <div className="compare-file-head">
                      <div>
                        <h3>{file.path}</h3>
                        {file.oldPath && <p className="compare-file-oldpath">renamed from {file.oldPath}</p>}
                      </div>
                      <span className={`compare-status compare-status-${file.status}`}>
                        {STATUS_LABELS[file.status] || file.status}
                      </span>
                    </div>
                    <p className="compare-file-explanation">{file.explanation}</p>
                    <p className="compare-file-recommendation">
                      <strong>Recommendation:</strong> {file.recommendation}
                    </p>

                    {collisions.length > 0 && (
                      <div className="compare-collisions">
                        <h4>{collisions.length} structural collision(s)</h4>
                        {collisions.map((collision, index) => (
                          <div className="compare-collision" key={`${collision.name}-${index}`}>
                            <div className="compare-collision-head">
                              <strong>{collision.kind} {collision.name}</strong>
                              <span>{collision.type} · line {collision.line}</span>
                            </div>
                            <div className="compare-collision-body">
                              {collision.base_content ? (
                                <div><em>Base</em><pre>{collision.base_content}</pre></div>
                              ) : null}
                              {collision.our_content ? (
                                <div><em>Ours</em><pre>{collision.our_content}</pre></div>
                              ) : null}
                              {collision.their_content ? (
                                <div><em>Theirs</em><pre>{collision.their_content}</pre></div>
                              ) : null}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default Compare
