import "./sidebar.css"
import { NavLink } from 'react-router-dom'
const links = [
    { to: '/', label: 'Dashboard', end: true },
    { to: '/conflicts', label: 'Conflicts' },
    { to: '/treediff', label: 'TreeDiff' },
    { to: '/suggestions', label: 'Suggestions' },
    { to: '/commitgraph', label: 'AST & Dependency Graph' },
    { to: '/compare', label: 'Compare' },
    { to: '/context', label: 'AI Context' },
    { to: '/history', label: 'History' },
]

function Sidebar({ open = false }) {
    return (
        <aside id="app-sidebar" className={open ? 'sidebar' : 'sidebar sidebar-hidden'} aria-label="Sidebar navigation">
            <div className="sidebar-header">
                <span className="sidebar-title">Commitment-Issues</span>
                <span className="sidebar-subtitle">Merge Conflict Resolver</span>
            </div>

            <nav>
                <ul className="sidebar-nav">
                    {links.map(({ to, label, end }) => (
                        <li key={to}>
                            <NavLink
                                to={to}
                                end={end}
                                className={({ isActive }) => `sidebar-link${isActive ? ' active' : ''}`}
                            >
                                <span>{label}</span>
                            </NavLink>
                        </li>
                    ))}
                </ul>
            </nav>
        </aside>
    )
}

export default Sidebar