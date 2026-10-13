import { Component } from 'react'

/**
 * Catch-all React error boundary. A render-time crash in one tab must never
 * blank out the entire SPA with no recovery path.
 */
class ErrorBoundary extends Component {
  constructor(props) {
    super(props)
    this.state = { error: null }
  }

  static getDerivedStateFromError(error) {
    return { error }
  }

  componentDidCatch(error, errorInfo) {
    console.error('Unhandled render error:', error, errorInfo)
  }

  render() {
    if (this.state.error) {
      return (
        <div className="app-error-boundary" role="alert">
          <h2>Something went wrong</h2>
          <p>{String(this.state.error?.message || this.state.error)}</p>
          <button type="button" onClick={() => window.location.assign('/')}>
            Back to dashboard
          </button>
        </div>
      )
    }
    return this.props.children
  }
}

export default ErrorBoundary
