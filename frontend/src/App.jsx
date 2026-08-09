import React, { useState } from 'react';
import SemanticExplorer from './components/SemanticExplorer';
import PromptPreview from './components/PromptPreview';
import './App.css';

export function App() {
  const [selectedFile, setSelectedFile] = useState(null);
  const [showPromptPanel, setShowPromptPanel] = useState(false);

  return (
    <div className="app">
      <header className="app-header">
        <div className="header-left">
          <h1>🔀 MergeGraph AI</h1>
          <span className="subtitle">Semantic Merge Conflict Analysis</span>
        </div>
        <div className="header-right">
          <button
            className="toggle-prompt-btn"
            onClick={() => setShowPromptPanel(!showPromptPanel)}
            title="Toggle prompt preview panel"
          >
            {showPromptPanel ? '✕' : '+'} Prompt
          </button>
        </div>
      </header>

      <div className="app-body">
        <div className="main-panel">
          <SemanticExplorer onFileSelect={setSelectedFile} />
        </div>

        {showPromptPanel && (
          <div className="prompt-panel">
            <PromptPreview file={selectedFile} />
          </div>
        )}
      </div>

      <footer className="app-footer">
        <div className="footer-left">
          Graph Server: <span className="status-online">● Online</span>
        </div>
        <div className="footer-right">
          <a href="https://github.com" target="_blank" rel="noopener noreferrer">
            GitHub
          </a>
          <span className="divider">•</span>
          <a href="/docs" target="_blank" rel="noopener noreferrer">
            Docs
          </a>
        </div>
      </footer>
    </div>
  );
}

export default App;