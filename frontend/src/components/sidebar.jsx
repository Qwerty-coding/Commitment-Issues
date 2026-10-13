import "./sidebar.css"
import { NavLink } from 'react-router-dom'
import {
    LayoutDashboard,
    AlertTriangle,
    GitCompare,
    Sparkles,
    Network,
    Scale,
    FileText,
    History as HistoryIcon
} from 'lucide-react'

const links = [
    { to: '/', label: 'Dashboard', Icon: LayoutDashboard, end: true },
    { to: '/conflicts', label: 'Conflicts', Icon: AlertTriangle },
    { to: '/treediff', label: 'TreeDiff', Icon: GitCompare },
    { to: '/suggestions', label: 'Suggestions', Icon: Sparkles },
    { to: '/commitgraph', label: 'AST & Dependency Graph', Icon: Network },
    { to: '/compare', label: 'Compare', Icon: Scale },
    { to: '/context', label: 'AI Context', Icon: FileText },
    { to: '/history', label: 'History', Icon: HistoryIcon },
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
                    {links.map(({ to, label, Icon, end }) => (
                        <li key={to}>
                            <NavLink
                                to={to}
                                end={end}
                                className={({ isActive }) => `sidebar-link${isActive ? ' active' : ''}`}
                            >
                                <Icon size={18} className="sidebar-icon" />
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