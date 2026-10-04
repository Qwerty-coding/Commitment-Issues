import './navbar.css'
import { useEffect, useState } from 'react'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function Navbar({ toggleSidebar }) {
    const [repoName, setRepoName] = useState("");
    const [currentBranch, setCurrentBranch] = useState("");

    useEffect(() => {
        const controller = new AbortController()

        fetch(`${API_BASE}/api/repository`, { signal: controller.signal })
            .then((response) => response.json())
            .then((payload) => {
                const data = payload?.data || {}
                setRepoName(data.name || "")
                setCurrentBranch(data.currentBranch || "")
            })
            .catch((error) => {
                if (error.name === 'AbortError') return
                console.log("Error fetching repository metadata", error)
            })

        return () => controller.abort()
    }, [])

    return (
        <nav className="navbar bg-dark navbar-expand-lg sticky-top">
            <div className="container-fluid">
                <button
                    type="button"
                    className="navbar-brand navbar-toggle"
                    onClick={toggleSidebar}
                    aria-label="Toggle sidebar"
                >
                    <i className="fa-solid fa-code-pull-request"></i> MergeSolver
                </button>
                <div className="subpart-div">
                    <ul className="navbar-nav subpart">
                        <li className="nav-item sidebar-item">
                            <span className="nav-link active" aria-current="page" style={{ color: 'white' }}><i className="fa-regular fa-folder"></i> {repoName || 'Repository'}</span>
                            <span className="tooltip">Repository</span>
                        </li>
                        <li className="nav-item sidebar-item">
                            <span className="nav-link active" aria-current="page" style={{ color: 'white' }}><i className="fa-solid fa-arrows-split-up-and-left"></i> {currentBranch || 'branch'}</span>
                            <span className="tooltip">Branch</span>
                        </li>
                    </ul>
                </div>
            </div>
        </nav>
    )
}

export default Navbar;