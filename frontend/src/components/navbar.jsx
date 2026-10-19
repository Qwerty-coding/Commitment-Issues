import './navbar.css'
import { useEffect, useState } from 'react'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import { useTheme } from '../lib/theme'
import { Menu, Folder, GitBranch, Sun, Moon } from 'lucide-react'

function Navbar({ toggleSidebar, sidebarOpen }) {
    const [repoName, setRepoName] = useState("")
    const [currentBranch, setCurrentBranch] = useState("")
    const [incomingBranch, setIncomingBranch] = useState("")
    const [refreshing, setRefreshing] = useState(false)
    const { theme, toggleTheme } = useTheme()

    const loadRepo = (signal) => {
        fetchJson(`${API_BASE}/api/repository`, { signal })
            .then((payload) => {
                const data = payload?.data || {}
                setRepoName(data.name || "")
                setCurrentBranch(data.currentBranch || "")
                setIncomingBranch(data.incomingBranch || "")
            })
            .catch((error) => {
                if (isAbort(error)) return
                console.warn("Error fetching repository metadata", error)
            })
    }

    useEffect(() => {
        const controller = new AbortController()
        loadRepo(controller.signal)
        return () => controller.abort()
    }, [])

    const handleRefresh = async () => {
        setRefreshing(true)
        try {
            await fetchJson(`${API_BASE}/api/analysis/refresh`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ repositoryRoot: '.', file: '' }),
            })
            loadRepo()
            window.dispatchEvent(new CustomEvent('ci:refresh-analysis'))
        } catch (err) {
            console.error('Refresh failed:', err)
        } finally {
            setRefreshing(false)
        }
    }

    return (
        <nav className="navbar" role="navigation" aria-label="Main Navigation">
            <div className="container-fluid">
                <div className="navbar-brand-group">
                    <button
                        type="button"
                        className="navbar-toggle"
                        onClick={toggleSidebar}
                        aria-label="Toggle navigation menu"
                        aria-expanded={sidebarOpen}
                        aria-controls="app-sidebar"
                    >
                        <Menu size={20} className="menu-icon" />
                        <span>Commitment-Issues</span>
                    </button>
                </div>
                <div className="subpart-div">
                    <ul className="subpart">
                        <li>
                            <div className="navbar-pill" title="Current Repository">
                                <Folder size={14} className="navbar-pill-icon" />
                                <span>{repoName || 'Repository'}</span>
                            </div>
                        </li>
                        <li>
                            <div className="navbar-pill" title="Current Branch">
                                <GitBranch size={14} className="navbar-pill-icon" />
                                <span>{currentBranch || 'branch'}</span>
                            </div>
                        </li>
                        {incomingBranch && (
                            <li>
                                <div className="navbar-pill" title={`Comparing: ${currentBranch} ↔ ${incomingBranch}`} style={{ borderColor: 'var(--brand, #2563eb)' }}>
                                    <span>vs {incomingBranch}</span>
                                </div>
                            </li>
                        )}
                        <li>
                            <button
                                type="button"
                                className="navbar-refresh-btn"
                                onClick={handleRefresh}
                                disabled={refreshing}
                                title="Re-scan workspace for conflicts and refresh analysis"
                                style={{
                                    padding: '6px 12px',
                                    fontSize: '13px',
                                    fontWeight: 500,
                                    borderRadius: '8px',
                                    border: '1px solid rgba(255, 255, 255, 0.15)',
                                    background: 'transparent',
                                    color: '#e6edf3',
                                    cursor: 'pointer',
                                    display: 'inline-flex',
                                    alignItems: 'center',
                                    gap: '6px',
                                    transition: 'all 0.15s ease',
                                }}
                            >
                                {refreshing ? 'Refreshing…' : 'Refresh Analysis'}
                            </button>
                        </li>
                        <li>
                            <button
                                type="button"
                                className="navbar-theme-toggle"
                                onClick={toggleTheme}
                                aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
                                title={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
                            >
                                {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
                            </button>
                        </li>
                    </ul>
                </div>
            </div>
        </nav>
    )
}

export default Navbar