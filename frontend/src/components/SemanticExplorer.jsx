import React, { useEffect, useRef, useState, useCallback } from 'react';
import cytoscape from 'cytoscape';
import dagre from 'cytoscape-dagre';
import expandCollapse from 'cytoscape-expand-collapse';
import 'cytoscape/dist/cytoscape.css';
import './SemanticExplorer.css';

// Register Cytoscape extensions
cytoscape.use(dagre);
cytoscape.use(expandCollapse);

export function SemanticExplorer() {
  const containerRef = useRef(null);
  const cyRef = useRef(null);
  const [selectedNode, setSelectedNode] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [focusMode, setFocusMode] = useState(false);

  // Fetch graph data from backend
  useEffect(() => {
    const fetchGraph = async () => {
      try {
        setLoading(true);
        const response = await fetch('http://localhost:8080/api/graph');
        const json = await response.json();

        if (!json.success) {
          throw new Error(json.error?.message || 'Failed to load graph');
        }

        initializeCytoscape(json.data);
      } catch (err) {
        setError(err.message);
        console.error('Graph fetch error:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchGraph();
  }, []);

  const initializeCytoscape = (graphDTO) => {
    if (!containerRef.current) return;

    // Convert DTO to Cytoscape format
    const elements = [];

    // Add nodes
    graphDTO.nodes?.forEach((node) => {
      elements.push({
        data: node.data,
        position: { x: Math.random() * 500, y: Math.random() * 500 },
      });
    });

    // Add edges
    graphDTO.edges?.forEach((edge) => {
      elements.push({
        data: edge.data,
      });
    });

    // Initialize Cytoscape
    const cy = cytoscape({
      container: containerRef.current,
      elements,
      style: getDefaultStyle(),
      layout: { name: 'dagre', directed: true, nodeSep: 50, rankSep: 50 },
      wheelSensitivity: 0.1,
      pixelRatio: 'auto',
    });

    // Bind events
    cy.on('tap', 'node', (event) => {
      setSelectedNode(event.target.id());
    });

    cy.on('tap', () => {
      if (cyRef.current?.elements().areNeighboursOf) {
        setSelectedNode(null);
      }
    });

    // Store reference
    cyRef.current = cy;

    // Auto-layout
    setTimeout(() => {
      cy.layout({ name: 'dagre', directed: true, nodeSep: 50, rankSep: 50 }).run();
    }, 100);
  };

  const handleExpand = useCallback(async (nodeId) => {
    if (!cyRef.current) return;

    try {
      const response = await fetch('http://localhost:8080/api/graph/expand', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nodeId }),
      });

      const json = await response.json();
      if (!json.success) {
        throw new Error(json.error?.message || 'Expand failed');
      }

      // Update graph with new data
      // In a real implementation, this would only add newly-discovered nodes,
      // not re-render the entire graph. For now, re-initialize.
      initializeCytoscape(json.data);
    } catch (err) {
      console.error('Expand error:', err);
    }
  }, []);

  const handleCollapse = useCallback(async (nodeId) => {
    if (!cyRef.current) return;

    try {
      const response = await fetch('http://localhost:8080/api/graph/collapse', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nodeId }),
      });

      const json = await response.json();
      if (!json.success) {
        throw new Error(json.error?.message || 'Collapse failed');
      }

      initializeCytoscape(json.data);
    } catch (err) {
      console.error('Collapse error:', err);
    }
  }, []);

  const handleFocus = useCallback(async () => {
    if (!selectedNode || !cyRef.current) return;

    try {
      setFocusMode(true);
      const response = await fetch('http://localhost:8080/api/graph/focus', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nodeId: selectedNode }),
      });

      const json = await response.json();
      if (!json.success) {
        throw new Error(json.error?.message || 'Focus failed');
      }

      initializeCytoscape(json.data);
    } catch (err) {
      console.error('Focus error:', err);
      setFocusMode(false);
    }
  }, [selectedNode]);

  const handleClearFocus = useCallback(() => {
    setFocusMode(false);
    // Re-fetch full graph
    const fetchGraph = async () => {
      try {
        const response = await fetch('http://localhost:8080/api/graph');
        const json = await response.json();
        if (json.success) {
          initializeCytoscape(json.data);
        }
      } catch (err) {
        console.error('Graph fetch error:', err);
      }
    };
    fetchGraph();
  }, []);

  if (loading) {
    return <div className="semantic-explorer loading">Loading semantic graph...</div>;
  }

  if (error) {
    return <div className="semantic-explorer error">Error: {error}</div>;
  }

  return (
    <div className="semantic-explorer">
      <div className="explorer-toolbar">
        <button
          onClick={() => selectedNode && handleExpand(selectedNode)}
          disabled={!selectedNode}
          title="Expand selected node to show children"
        >
          ↓ Expand
        </button>
        <button
          onClick={() => selectedNode && handleCollapse(selectedNode)}
          disabled={!selectedNode}
          title="Collapse selected node to hide children"
        >
          ↑ Collapse
        </button>
        <button
          onClick={handleFocus}
          disabled={!selectedNode}
          title="Focus on conflict scope around selected node"
        >
          🎯 Focus
        </button>
        {focusMode && (
          <button
            onClick={handleClearFocus}
            className="focus-active"
            title="Exit focus mode"
          >
            ✕ Clear Focus
          </button>
        )}
      </div>

      <div className="explorer-container" ref={containerRef} />

      {selectedNode && (
        <Inspector nodeId={selectedNode} cy={cyRef.current} />
      )}
    </div>
  );
}

// Inspector panel — shows metadata for selected node
function Inspector({ nodeId, cy }) {
  const [metadata, setMetadata] = useState(null);

  useEffect(() => {
    if (!cy) return;

    const node = cy.$id(nodeId);
    if (node.length === 0) {
      setMetadata(null);
      return;
    }

    const data = node.data();
    setMetadata({
      id: data.id,
      label: data.label,
      kind: data.kind,
      status: data.status,
      line: data.line,
      incomingEdges: cy.edges().stdFilter((e) => e.target().id() === nodeId).length,
      outgoingEdges: cy.edges().stdFilter((e) => e.source().id() === nodeId).length,
    });
  }, [nodeId, cy]);

  if (!metadata) return null;

  return (
    <div className="inspector-panel">
      <h3>Inspector</h3>
      <div className="inspector-content">
        <div className="inspector-field">
          <label>Label</label>
          <code>{metadata.label}</code>
        </div>
        <div className="inspector-field">
          <label>Kind</label>
          <span>{metadata.kind}</span>
        </div>
        <div className="inspector-field">
          <label>Status</label>
          <span className={`status-${metadata.status?.toLowerCase()}`}>
            {metadata.status}
          </span>
        </div>
        {metadata.line > 0 && (
          <div className="inspector-field">
            <label>Line</label>
            <span>{metadata.line}</span>
          </div>
        )}
        <div className="inspector-field">
          <label>Edges</label>
          <span>
            ↓ {metadata.outgoingEdges} ↑ {metadata.incomingEdges}
          </span>
        </div>
      </div>
    </div>
  );
}

// Default Cytoscape stylesheet
function getDefaultStyle() {
  return [
    {
      selector: 'node',
      style: {
        'background-color': '#3B82F6',
        'label': 'data(label)',
        'text-valign': 'center',
        'text-halign': 'center',
        'width': '60px',
        'height': '60px',
        'font-size': '11px',
        'color': '#fff',
        'text-wrap': 'wrap',
        'text-max-width': '55px',
      },
    },
    {
      selector: 'node[parent]',
      style: {
        'background-color': '#8B5CF6',
      },
    },
    {
      selector: 'node[status="collision"]',
      style: {
        'background-color': '#EF4444',
      },
    },
    {
      selector: 'node[status="added"]',
      style: {
        'background-color': '#22C55E',
      },
    },
    {
      selector: 'node[status="updated"]',
      style: {
        'background-color': '#F59E0B',
      },
    },
    {
      selector: 'node[status="deleted"]',
      style: {
        'background-color': '#6B7280',
      },
    },
    {
      selector: 'node[status="file"]',
      style: {
        'background-color': '#1E293B',
        'width': '100px',
        'height': '40px',
        'shape': 'rectangle',
      },
    },
    {
      selector: 'node:selected',
      style: {
        'border-width': '3px',
        'border-color': '#fff',
        'background-color': '#fff',
        'color': '#000',
      },
    },
    {
      selector: 'edge',
      style: {
        'line-color': '#94A3B8',
        'target-arrow-color': '#94A3B8',
        'target-arrow-shape': 'triangle',
        'width': '2px',
        'curve-style': 'bezier',
      },
    },
    {
      selector: 'edge[type="CALLS"]',
      style: {
        'line-color': '#3B82F6',
        'target-arrow-color': '#3B82F6',
      },
    },
  ];
}

export default SemanticExplorer;