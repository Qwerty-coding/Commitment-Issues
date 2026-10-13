import './navbar.css'
import { useEffect, useState } from 'react'
import { API_BASE, fetchJson, isAbort } from '../lib/api'
import { Menu, GitPullRequest, Folder, GitBranch } from 'lucide-react'

function Navbar({ toggleSidebar, sidebarOpen }) {
    const [repoName, setRepoName] = useState("")
    const [currentBranch, setCurrentBranch] = useState("")

    useEffect(() => {
        const controller = new AbortController()

        fetchJson(`${API_BASE}/api/repository`, { signal: controller.signal })
            .then((payload) => {
                const data = payload?.data || {}
                setRepoName(data.name || "")
                setCurrentBranch(data.currentBranch || "")
            })
            .catch((error) => {
                if (isAbort(error)) return
                console.warn("Error fetching repository metadata", error)
            })

        return () => controller.abort()
    }, [])

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
                        <GitPullRequest size={20} className="brand-icon" />
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
                    </ul>
                </div>
            </div>
        </nav>
    )
}

export default Navbar