import { Link } from 'react-router-dom'

function NotFound() {
  return (
    <div className="not-found-page">
      <h1>404 — Page not found</h1>
      <p>The view you requested does not exist.</p>
      <Link to="/">Back to dashboard</Link>
    </div>
  )
}

export default NotFound
