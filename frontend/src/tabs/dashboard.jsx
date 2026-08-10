import "./dashboard.css";
import {useEffect, useState} from "react";

function Dashboard() {

        const [repoName, setRepoName] = useState("");
        const [currentBranch, setCurrentBranch] = useState("");
        const[incomingBranch, setIncomingBranch] = useState("");
    
    
        useEffect(()=>{
            fetch("http://localhost:8080/api/repository")
            .then((response)=>response.json())
            .then((data) => {
                console.log(data)
                setRepoName(data.name)
                setCurrentBranch(data.currentBranch)
                setIncomingBranch(data.incomingBranch)
            })
            .catch((error) =>{
                console.log("Error fetching Laboratory", error);
            })
        },[])

    return (
        <div className="dashboard-page">

            {/* Welcome Section */}
            <div className="welcome-section">
                <h1>Welcome back, Mahak 👋</h1>

                <p>
                    Here's what's happening with your repository.
                </p>
            </div>


            {/* Repository Card */}
            <div className="repository-card">

                <div className="repository-icon">
                    <i className="fa-regular fa-folder"></i>
                </div>

                <div className="repository-content">

                    <h2>{repoName}</h2>

                    <div className="branch-flow">

                        <div className="branch current-branch">
                            <i className="fa-solid fa-code-branch"></i>
                            <span>{currentBranch}</span>
                        </div>

                        <i className="fa-solid fa-arrow-right branch-arrow"></i>

                        <div className="branch main-branch">
                            <i className="fa-solid fa-code-branch"></i>
                            <span>{incomingBranch}</span>
                        </div>

                    </div>

                    <div className="repository-divider"></div>

                    <div className="workspace-text">
                        <i className="fa-solid fa-code"></i>
                        <span>Your current merge workspace</span>
                    </div>

                </div>

            </div>

        </div>
    );
}

export default Dashboard;