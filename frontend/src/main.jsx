import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import './dark-overrides.css'
import App from './App.jsx'
import { applyTheme } from './lib/theme.js'

// Apply the persisted/OS theme before first paint to avoid a light-mode flash.
try {
  const stored = window.localStorage.getItem('commitissues-theme')
  if (stored === 'dark' || stored === 'light') {
    applyTheme(stored)
  } else if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    applyTheme('dark')
  }
} catch {
  // localStorage unavailable; default light theme from CSS applies.
}

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
