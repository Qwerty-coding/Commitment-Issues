import "./sidebar.css"
import {useNavigate} from "react-router-dom"

function Sidebar(){
    const navigate = useNavigate();

    return(
    <div className="sidebar">
        <div className="sidebar-title">
            MergeSolver
        </div>

        <ul>
            <li>
                <div onClick={()=>{navigate("/")}}>
                    <i className="fa-solid fa-house"></i>
                    Dashboard
                </div>
            </li>

            <li>
                <div onClick={()=>{navigate("/conflicts")}}>
                    <i className="fa-solid fa-triangle-exclamation"></i>
                    Conflicts
                </div>
            </li>

            <li>
                <div onClick={()=>{navigate("/treediff")}}>
                    <i className="fa-solid fa-code-compare"></i>
                    TreeDiff
                </div>
            </li>

            <li>
                <div onClick={()=>{navigate("/suggestions")}}>
                    <i className="fa-solid fa-wand-magic-sparkles"></i>
                    Suggestions
                </div>
            </li>

            <li>
                <div onClick={()=>{navigate("/commitgraph")}}>
                    <i className="fa-solid fa-code-branch"></i>
                    Commit Graph
                </div>
            </li>

            <li>
                <div onClick={()=>{navigate("/history")}}>
                    <i className="fa-solid fa-clock-rotate-left"></i>
                    History
                </div>
            </li>
        </ul>
    </div>
    )
}

export default Sidebar;