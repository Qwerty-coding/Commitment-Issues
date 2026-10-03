import './navbar.css'
import { useEffect, useState } from 'react'

const API_BASE = import.meta.env.VITE_API_BASE || ''

function Navbar({ toggleSidebar }) {
    const [repoName, setRepoName] = useState("");
    const [currentBranch, setCurrentBranch] = useState("");


    useEffect(() => {
        fetch(`${API_BASE}/api/repository`)
            .then((response) => response.json())
            .then((payload) => {
                const data = payload?.data || {}
                setRepoName(data.name || "")
                setCurrentBranch(data.currentBranch || "")
            })
            .catch((error) => {
                console.log("Error fetching repository metadata", error)
            })
    }, [])

    return (
        <nav className="navbar bg-dark navbar-expand-lg sticky-top">
            <div className="container-fluid">
                <span style={{ color: 'white' }} onClick={toggleSidebar}>
                    <i className="fa-solid fa-code-pull-request"></i> MergeSolver
                </span>
                <div className="subpart-div">
                    <ul className="navbar-nav subpart">
                        <li className="nav-item sidebar-item">
                            <a className="nav-link active" aria-current="page" href="#" style={{ color: 'white' }}><i className="fa-regular fa-folder"></i> {repoName || 'Repository'}</a>
                            <span className="tooltip">Repository</span>
                        </li>
                        <li className="nav-item sidebar-item">
                            <a className="nav-link active" aria-current="page" href="#" style={{ color: 'white' }}><i className="fa-solid fa-arrows-split-up-and-left"></i> {currentBranch || 'branch'}</a>
                            <span className="tooltip">Branch</span>
                        </li>
                    </ul>
                </div>
            </div>
        </nav>
    )
}

export default Navbar;