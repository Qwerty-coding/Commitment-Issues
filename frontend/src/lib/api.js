/**
 * Centralized API access for all tabs: one API_BASE source, one JSON
 * fetch helper that enforces `response.ok` so 404/500 bodies never get
 * parsed by .json() with confusing results, and a shared AbortError guard.
 */

export const API_BASE = import.meta.env.VITE_API_BASE || ''

/**
 * Fetch a URL and parse its JSON body.
 * Throws TypeError-like Error with .status and .code when the response is
 * not ok or the body is not the expected { success, data, error } shape.
 */
export async function fetchJson(url, opts = {}) {
  const response = await fetch(url, opts)

  let payload = null
  try {
    payload = await response.json()
  } catch {
    // Non-JSON body (proxy error page, empty body, etc.)
  }

  if (!response.ok || (payload && payload.success === false)) {
    const failure = new Error(
      payload?.error?.message || `Request failed (HTTP ${response.status})`,
    )
    failure.status = response.status
    failure.code = payload?.error?.code || ''
    throw failure
  }
  return payload
}

/** True when err is a fetch AbortError (normal on unmount/redirect). */
export function isAbort(error) {
  return error?.name === 'AbortError'
}
