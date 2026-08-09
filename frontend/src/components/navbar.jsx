import "./navbar.css";
import Sidebar from "./sidebar.jsx"
import {useEffect, useState} from "react";


function Navbar({toggleSidebar}){
    const [repoName, setRepoName] = useState("");
    const [currentBranch, setCurrentBranch] = useState("");


    useEffect(()=>{
        fetch("http://localhost:8080/api/repository")
        .then((response)=>response.json())
        .then((data) => {
            console.log(data)
            setRepoName(data.name)
            setCurrentBranch(data.currentBranch)
        })
        .catch((error) =>{
            console.log("Error fetching Laboratory", error);
        })
    },[])

    return(
        <>
        <nav className="navbar bg-dark navbar-expand-lg sticky-top">
            <div className="container-fluid">
                <span style={{"color":"white"}} onClick={toggleSidebar
                }>
                    <i className="fa-solid fa-code-pull-request"></i> MergeSolver
                </span>
                <div className="subpart-div">
                    <ul className="navbar-nav subpart">
                        <li className="nav-item sidebar-item">
                            <a className="nav-link active" aria-current="page" href="#" style={{"color":"white"}}><i class="fa-regular fa-folder"></i> {repoName}</a>
                            <span className="tooltip">Repository</span>
                        </li>
                        <li className="nav-item sidebar-item">
                            <a className="nav-link active" aria-current="page" href="#" style={{"color":"white"}}><i class="fa-solid fa-arrows-split-up-and-left"></i> {currentBranch}</a>
                            <span className="tooltip">Branch</span>
                        </li>
                    </ul>
                </div>
                
            </div>
        </nav>

        </>
    )
}

export default Navbar;