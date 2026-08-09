import React, { useEffect, useState } from 'react';
import './PromptPreview.css';

export function PromptPreview({ file }) {
  const [promptContext, setPromptContext] = useState(null);
  const [statistics, setStatistics] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [showStatistics, setShowStatistics] = useState(false);

  useEffect(() => {
    if (!file) {
      setPromptContext(null);
      setStatistics(null);
      return;
    }

    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);

        // Fetch prompt context
        const promptResponse = await fetch(
          `http://localhost:8080/api/prompt?file=${encodeURIComponent(file)}`
        );
        const promptJson = await promptResponse.json();

        if (promptJson.success) {
          setPromptContext(promptJson.data);
        } else {
          setError(promptJson.error?.message || 'Failed to load prompt');
        }

        // Fetch statistics
        const statsResponse = await fetch(
          `http://localhost:8080/api/prompt/statistics?file=${encodeURIComponent(file)}`
        );
        const statsJson = await statsResponse.json();

        if (statsJson.success) {
          setStatistics(statsJson.data);
        }
      } catch (err) {
        setError(err.message);
        console.error('Fetch error:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [file]);

  if (!file) {
    return (
      <div className="prompt-preview empty">
        Select a conflict to preview the AI prompt
      </div>
    );
  }

  if (loading) {
    return (
      <div className="prompt-preview loading">
        <div className="spinner" />
        Loading prompt context...
      </div>
    );
  }

  if (error) {
    return <div className="prompt-preview error">Error: {error}</div>;
  }

  if (!promptContext) {
    return (
      <div className="prompt-preview empty">
        No prompt context generated for {file}
      </div>
    );
  }

  return (
    <div className="prompt-preview">
      <div className="preview-header">
        <h2>Prompt Preview</h2>
        <button
          className="stats-toggle"
          onClick={() => setShowStatistics(!showStatistics)}
          title="Toggle token statistics"
        >
          📊 Statistics
        </button>
      </div>

      {showStatistics && statistics && (
        <div className="statistics-panel">
          <div className="stat-row">
            <span className="stat-label">Scoped Tokens:</span>
            <span className="stat-value">{statistics.estimatedTokens}</span>
          </div>
          <div className="stat-row">
            <span className="stat-label">Full Payload Tokens:</span>
            <span className="stat-value">{statistics.fullPayloadTokens}</span>
          </div>
          <div className="stat-row">
            <span className="stat-label">Reduction:</span>
            <span className="stat-value reduction-positive">
              {statistics.reductionPercentage.toFixed(1)}%
            </span>
          </div>
          <div className="stat-row">
            <span className="stat-label">Selected Nodes:</span>
            <span className="stat-value">{statistics.selectedNodes}</span>
          </div>
          <div className="stat-row">
            <span className="stat-label">Excluded Nodes:</span>
            <span className="stat-value">{statistics.excludedNodes}</span>
          </div>
        </div>
      )}

      <div className="preview-content">
        <div className="content-section">
          <h3>Repository Summary</h3>
          <p className="monospace">
            {promptContext.repositorySummary || '(none)'}
          </p>
        </div>

        {promptContext.files && promptContext.files.length > 0 && (
          <div className="content-section">
            <h3>Files in Scope</h3>
            <ul className="file-list">
              {promptContext.files.map((f) => (
                <li key={f}>{f}</li>
              ))}
            </ul>
          </div>
        )}

        {promptContext.functions && promptContext.functions.length > 0 && (
          <div className="content-section">
            <h3>Relevant Functions ({promptContext.functions.length})</h3>
            <div className="functions-list">
              {promptContext.functions.map((fn, idx) => (
                <div key={idx} className="function-item">
                  <div className="function-header">
                    <code className="function-name">{fn.name}</code>
                    <span className="function-line">Line {fn.line}</span>
                  </div>
                  <pre className="function-code">{fn.content}</pre>
                </div>
              ))}
            </div>
          </div>
        )}

        {promptContext.imports && promptContext.imports.length > 0 && (
          <div className="content-section">
            <h3>Imports</h3>
            <ul className="imports-list">
              {promptContext.imports.map((imp, idx) => (
                <li key={idx}>{imp}</li>
              ))}
            </ul>
          </div>
        )}

        <div className="content-section">
          <h3>Full Prompt Context</h3>
          <div className="full-context">
            <p>{promptContext.context || '(empty)'}</p>
          </div>
        </div>
      </div>

      <div className="preview-actions">
        <button
          className="action-button send-to-ai"
          onClick={() => handleSendToAI(promptContext)}
        >
          Send to AI
        </button>
        <button
          className="action-button copy-prompt"
          onClick={() => handleCopyPrompt(promptContext.context)}
        >
          Copy Prompt
        </button>
      </div>
    </div>
  );
}

function handleSendToAI(promptContext) {
  console.log('Sending to AI:', promptContext);
  // TODO: Implement actual AI request
  alert('AI integration not yet implemented. Check console.');
}

function handleCopyPrompt(context) {
  navigator.clipboard.writeText(context).then(() => {
    alert('Prompt copied to clipboard');
  }).catch((err) => {
    console.error('Copy failed:', err);
  });
}

export default PromptPreview;