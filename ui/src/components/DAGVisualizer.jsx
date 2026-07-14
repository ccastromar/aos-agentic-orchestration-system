import React, { useMemo } from 'react';
import ReactFlow, { Background, Controls } from 'reactflow';
import 'reactflow/dist/style.css';
import dagre from 'dagre';

const nodeWidth = 172;
const nodeHeight = 36;

const getLayoutedElements = (nodes, edges, direction = 'TB') => {
  const dagreGraph = new dagre.graphlib.Graph();
  dagreGraph.setDefaultEdgeLabel(() => ({}));
  
  const isHorizontal = direction === 'LR';
  dagreGraph.setGraph({ rankdir: direction });

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: nodeWidth, height: nodeHeight });
  });

  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target);
  });

  dagre.layout(dagreGraph);

  nodes.forEach((node) => {
    const nodeWithPosition = dagreGraph.node(node.id);
    node.targetPosition = isHorizontal ? 'left' : 'top';
    node.sourcePosition = isHorizontal ? 'right' : 'bottom';
    
    node.position = {
      x: nodeWithPosition.x - nodeWidth / 2,
      y: nodeWithPosition.y - nodeHeight / 2,
    };
    return node;
  });

  return { nodes, edges };
};

const nodeTypes = {};
const edgeTypes = {};

export default function DAGVisualizer({ pipelineSteps = [], taskEvents = [], mode = 'dag' }) {
  const { nodes: initialNodes, edges: initialEdges } = useMemo(() => {
    const nodes = [];
    const edges = [];
    
    // Parse the steps (from dag_structure event JSON)
    pipelineSteps.forEach((step, index) => {
      const id = step.id || step.tool || 'analyst';
      const label = step.tool || step.human_approval || (step.analyst ? 'LLM Analyst' : 'Unknown Step');
      
      // Determine status from taskEvents
      let status = 'pending';
      let backgroundColor = 'rgba(255,255,255,0.05)';
      let borderColor = 'rgba(255,255,255,0.1)';
      
      // Check events to see if tool executed
      const toolOkEvent = taskEvents.find(e => e.action === `tool ${step.tool}` && e.status === 'ok');
      const toolErrorEvent = taskEvents.find(e => e.action === `tool ${step.tool}` && e.status === 'error');
      const awaitHumanEvent = taskEvents.find(e => e.action === `await_human` && e.status === step.human_approval);
      const isApproved = taskEvents.find(e => e.action === `human_decision` && e.status === 'approved' && e.duration === step.human_approval);
      
      // For analyst steps
      const analystSummaryEvent = taskEvents.find(e => e.component === 'Analyst' && e.action === 'summary');

      if (toolErrorEvent) {
        status = 'error';
        backgroundColor = 'rgba(239, 68, 68, 0.2)';
        borderColor = '#ef4444';
      } else if (toolOkEvent || (step.analyst && analystSummaryEvent) || isApproved) {
        status = 'success';
        backgroundColor = 'rgba(16, 185, 129, 0.2)';
        borderColor = '#10b981';
      } else if (awaitHumanEvent && !isApproved) {
        status = 'paused';
        backgroundColor = 'rgba(245, 158, 11, 0.2)';
        borderColor = '#f59e0b';
      }
      
      nodes.push({
        id,
        data: { label: label },
        className: `dag-node status-${status} ${step.human_approval ? 'is-gate' : ''}`,
        style: {
          background: backgroundColor,
          color: '#fff',
          border: `1px solid ${borderColor}`,
          borderRadius: '8px',
          padding: '10px',
          fontSize: '12px'
        }
      });
      
      // Link nodes sequentially if linear pipeline
      const isLinear = mode !== 'dag';
      if (isLinear && index > 0) {
        const prevStep = pipelineSteps[index - 1];
        const prevId = prevStep.id || prevStep.tool || 'analyst';
          edges.push({
            id: `${prevId}-${id}`,
            source: prevId,
            target: id,
            animated: status === 'pending',
            style: { stroke: 'var(--primary)', strokeWidth: 2 }
          });
        } else if (step.depends_on && Array.isArray(step.depends_on)) {
          step.depends_on.forEach((dep) => {
            edges.push({
              id: `${dep}-${id}`,
              source: dep,
              target: id,
              animated: status === 'pending',
              style: { stroke: 'var(--primary)', strokeWidth: 2 }
            });
        });
      }
    });
    
    return getLayoutedElements(nodes, edges, 'TB');
  }, [pipelineSteps, taskEvents]);
  
  return (
    <div style={{ position: 'relative', height: '350px', width: '100%', border: '1px solid var(--border-light)', borderRadius: '12px', overflow: 'hidden', background: 'var(--bg-glass)' }}>
      <ReactFlow 
        nodes={initialNodes} 
        edges={initialEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
      >
        <Background color="rgba(255,255,255,0.1)" gap={16} />
        <Controls />
      </ReactFlow>
    </div>
  );
}
