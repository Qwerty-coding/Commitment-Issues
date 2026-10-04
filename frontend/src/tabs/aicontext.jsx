import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import './aicontext.css'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function AIContext() {
  const location = useLocation()
  const query = new URLSearchParams(location.search)
  const requestedFile = query.get('file') || ''

  const [contexts, setContexts] = useState([])
  const [selectedKey, setSelectedKey] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()

    fetch(`${API_BASE}/api/prompt`, { signal: controller.signal })
      .then((response) => response.json())
      .then((payload) => {
        if (!payload?.success) {
          throw new Error(payload?.error?.message || 'Failed to load prompt context')
        }
        const data = Array.isArray(payload.data) ? payload.data : []
        setContexts(data)
        if (data.length > 0) {
          const requested = data.find((ctx) => contextKey(ctx) === requestedFile)
          setSelectedKey(requested ? contextKey(requested) : contextKey(data[0]))
        }
      })
      .catch((err) => {
        if (err.name === 'AbortError') return
        console.error('Error loading AI context:', err)
        setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })

    return () => controller.abort()
  }, [requestedFile])

  const selected = contexts.find((ctx) => contextKey(ctx) === selectedKey)
  const readme = (selected?.repositoryReadme || '').trim()

  if (loading) {
    return <h2>Loading AI context...</h2>
  }

  if (error) {
    return <h2>Error loading AI context: {error}</h2>
  }

  return (
    <div className="aicontext-page">
      <h1>AI Context</h1>
      <p className="aicontext-subtitle">
        The exact project context sent to the AI resolver for each conflicted file.
      </p>

      {contexts.length === 0 ? (
        <p>No prompt context available.</p>
      ) : (
        <div className="aicontext-grid">
          <aside className="aicontext-list">
            {contexts.map((ctx) => (
              <button
                key={contextKey(ctx)}
                className={`aicontext-list-item ${contextKey(ctx) === selectedKey ? 'active' : ''}`}
                onClick={() => setSelectedKey(contextKey(ctx))}
              >
                <span>{contextKey(ctx)}</span>
                <span>≈ {ctx.estimatedTokens ?? 0} tokens</span>
              </button>
            ))}
          </aside>

          <main className="aicontext-detail">
            {selected ? (
              <>
                <div className="aicontext-detail-header">
                  <div>
                    <h2>{contextKey(selected)}</h2>
                    <p>{selected.repositorySummary}</p>
                  </div>
                  <div className="aicontext-stats">
                    <span>≈ {selected.estimatedTokens ?? 0} tokens</span>
                    <span>{selected.functions?.length ?? 0} functions</span>
                    <span>{selected.imports?.length ?? 0} imports</span>
                  </div>
                </div>

                {readme ? (
                  <section className="aicontext-readme">
                    <h3>Repository README context</h3>
                    <p className="aicontext-readme-note">
                      Included as pre-context so the resolver understands the project.
                    </p>
                    <pre className="aicontext-readme-body">{readme}</pre>
                  </section>
                ) : null}

                <section>
                  <h3>Files in scope</h3>
                  {selected.files?.length ? (
                    <ul className="aicontext-filelist">
                      {selected.files.map((fileName) => (
                        <li key={fileName}>{fileName}</li>
                      ))}
                    </ul>
                  ) : (
                    <p>No files in scope.</p>
                  )}
                </section>

                <section>
                  <h3>Imports</h3>
                  {selected.imports?.length ? (
                    <ul className="aicontext-filelist">
                      {selected.imports.map((imp) => (
                        <li key={imp}>{imp}</li>
                      ))}
                    </ul>
                  ) : (
                    <p>No imports extracted for this file yet.</p>
                  )}
                </section>

                <section>
                  <h3>Relevant functions</h3>
                  {selected.functions?.length ? (
                    <div className="aicontext-function-list">
                      {selected.functions.map((fn, index) => (
                        <div className="aicontext-function" key={`${fn.name}-${fn.line}-${index}`}>
                          <div className="aicontext-function-header">
                            <span className="aicontext-function-title">
                              {fn.kind} {fn.name}
                            </span>
                            <span className="aicontext-function-meta">
                              line {fn.line}
                              {fn.scope ? ` · ${fn.scope}` : ''}
                              {fn.signature ? ` · ${fn.signature}` : ''}
                            </span>
                          </div>
                          <pre className="aicontext-code">{fn.content}</pre>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p>No functions in scope for this conflict.</p>
                  )}
                </section>

                <details className="aicontext-raw">
                  <summary>Raw assembled context (what the provider receives)</summary>
                  <pre className="aicontext-code">{selected.context}</pre>
                </details>
              </>
            ) : (
              <p>Select a file to view its AI context.</p>
            )}
          </main>
        </div>
      )}
    </div>
  )
}

function contextKey(ctx) {
  return ctx?.files?.[0] || '(unknown file)'
}

export default AIContext
