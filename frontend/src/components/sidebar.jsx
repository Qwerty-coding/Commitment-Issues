import "./sidebar.css"
import { NavLink } from 'react-router-dom'

const links = [
    { to: '/', label: 'Dashboard', icon: 'fa-solid fa-house', end: true },
    { to: '/conflicts', label: 'Conflicts', icon: 'fa-solid fa-triangle-exclamation' },
    { to: '/treediff', label: 'TreeDiff', icon: 'fa-solid fa-code-compare' },
    { to: '/suggestions', label: 'Suggestions', icon: 'fa-solid fa-wand-magic-sparkles' },
    { to: '/commitgraph', label: 'Commit Graph', icon: 'fa-solid fa-code-branch' },
    { to: '/compare', label: 'Compare', icon: 'fa-solid fa-scale-balanced' },
    { to: '/context', label: 'AI Context', icon: 'fa-solid fa-file-lines' },
    { to: '/history', label: 'History', icon: 'fa-solid fa-clock-rotate-left' },
]

function Sidebar(){
    return(
    <div className="sidebar">
        <div className="sidebar-title">
            MergeSolver
        </div>

        <ul>
            {links.map((link) => (
                <li key={link.to}>
                    <NavLink
                        to={link.to}
                        end={link.end}
                        className={({isActive}) => `sidebar-link${isActive ? ' active' : ''}`}
                    >
                        <i className={link.icon}></i>
                        {link.label}
                    </NavLink>
                </li>
            ))}
        </ul>
    </div>
    )
}

export default Sidebar;