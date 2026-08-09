import { useEffect, useState } from "react";
import "./suggestions.css"

function Suggestions() {
    const [details, setDetails] = useState(null);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        fetch("http://localhost:8080/api/suggestions")
            .then((response) => response.json())
            .then((data) => {
                console.log(data);
                setDetails(data);
                setLoading(false);
            })
            .catch((error) => {
                console.error("Error fetching suggestions:", error);
                setLoading(false);
            });
    }, []);

    if (loading) {
        return <h2>Loading suggestions...</h2>;
    }

    return (
        <div className="suggestions-page">

            {/* PAGE HEADER */}
            <div className="suggestions-header">

                <h1>AI Suggestion</h1>

                <p>
                    AI-generated resolution suggestion for the current conflict
                </p>

            </div>
            {/* INFO CARDS */}
            <div className="suggestion-info-row">

                {/* REPOSITORY */}
                <div className="suggestion-info-card">

                    <div className="info-icon">
                        <i className="fa-regular fa-folder"></i>
                    </div>

                    <div>
                        <span className="info-label">
                            Repository
                        </span>

                        <h4>
                            {details?.repo}
                        </h4>
                    </div>

                </div>


                {/* CURRENT BRANCH */}
                <div className="suggestion-info-card">

                    <div className="info-icon">
                        <i className="fa-solid fa-code-branch"></i>
                    </div>

                    <div>
                        <span className="info-label">
                            Current Branch
                        </span>

                        <h4>
                            {details?.current}
                        </h4>
                    </div>

                </div>


                {/* CONFIDENCE */}
                <div className="suggestion-info-card">

                    <div className="info-icon">
                        <i className="fa-solid fa-shield-halved"></i>
                    </div>

                    <div>
                        <span className="info-label">
                            Confidence
                        </span>

                        <h4 className="confidence-value">
                            {details?.confidence}
                        </h4>
                    </div>

                </div>
                </div>

                {/* CONFLICTED FILE */}

                <div className="conflicted-file-card">

                    <div className="conflicted-file-header">

                        <div className="conflicted-file-info">

                            <div className="file-icon">
                                <i className="fa-regular fa-file-lines"></i>
                            </div>

                            <div>
                                <span className="info-label">
                                    Conflicted File
                                </span>

                                <h3>
                                    {details?.file}
                                </h3>
                            </div>

                        </div>


                        <div className="confidence-badge">
                            High Confidence
                        </div>

                    </div>


                    <div className="confidence-bar-container">

                        <div className="confidence-bar">

                            <div
                                className="confidence-fill"
                                style={{ width: "94%" }}
                            ></div>

                        </div>

                        <span className="confidence-percent">
                            94%
                        </span>

                    </div>

                </div>

                {/* AI SUGGESTION */}

                <div className="ai-suggestion-card">

                    <div className="ai-suggestion-title">

                        <div className="ai-suggestion-icon">
                            <i className="fa-solid fa-wand-magic-sparkles"></i>
                        </div>

                        <div>
                            <span className="info-label">
                                AI Suggested Resolution
                            </span>

                            <h3>
                                Suggestion
                            </h3>
                        </div>

                    </div>


                    <div className="ai-suggestion-content">

                        <p>
                            {details?.suggestion}
                        </p>

                    </div>

                </div>

            </div>

        

    )
}

export default Suggestions;
