import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { format } from 'date-fns';
import { BrainCircuit, Wrench, CheckCircle, AlertTriangle } from 'lucide-react';
import DAGVisualizer from './DAGVisualizer';

const Timeline = ({ taskId, onApprovalNeeded }) => {
  const [events, setEvents] = useState([]);
  const [status, setStatus] = useState('pending'); // pending, ok, error
  const [finalData, setFinalData] = useState(null);

  useEffect(() => {
    if (!taskId) return;

    setEvents([]);
    setStatus('pending');
    setFinalData(null);

    const eventSource = new EventSource(`/ui/task/events?id=${taskId}`);

    eventSource.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data);
        setEvents((prev) => [...prev, ev]);
      } catch (err) {
        console.error("Error parsing event", err);
      }
    };

    const statusPollingInterval = setInterval(async () => {
      try {
        const tskRes = await fetch(`/task?id=${taskId}`);
        if (tskRes.ok) {
          const data = await tskRes.json();
          if (data.status !== 'pending' && data.status !== 'await_human' && data.status !== 'await_clarification') {
            setStatus(data.status);
            if (data.data) setFinalData(data.data);
            if (data.error) setFinalData({ error: data.error });
            
            // Only close polling and stream if the task is completely finished.
            if (['ok', 'success', 'error'].includes(data.status)) {
              clearInterval(statusPollingInterval);
              // Slight delay to ensure trailing events (like analyst summaries) arrive
              setTimeout(() => eventSource.close(), 1000);
            }
          } else if (data.status === 'await_human' || data.status === 'await_clarification') {
            setStatus(data.status);
            setFinalData(null); // Clear final data if we are awaiting human input
          }
        }
      } catch (e) {
        console.error("Status polling error", e);
      }
    }, 3000);

    return () => {
      eventSource.close();
      clearInterval(statusPollingInterval);
    };
  }, [taskId]);

  useEffect(() => {
    if (events.length === 0) return;
    const lastEvent = events[events.length - 1];
    
    if (lastEvent.Kind === 'await_human') {
      if (lastEvent.Message === 'clarification') {
        const clarificationEvent = events.slice().reverse().find(x => x.Kind === 'await_clarification');
        onApprovalNeeded(taskId, "clarification", clarificationEvent ? clarificationEvent.Message : "Please provide the missing parameters.");
      } else {
        let ctxString = "Please review and approve this action.";
        if (lastEvent.Data) {
          try {
            const ctxData = JSON.parse(lastEvent.Data);
            const filtered = Object.entries(ctxData)
              .filter(([k]) => !k.includes('._') && !k.includes('.statusCode') && !k.includes('.message') && !k.includes('.ok') && !k.includes('.success') && !k.includes('.status'))
              .map(([k,v]) => {
                const shortKey = k.split('.').pop();
                return `- ${shortKey}: ${JSON.stringify(v)}`;
              });
            ctxString = "Pipeline Context: \n" + filtered.join("\n");
          } catch(e) {
            ctxString = lastEvent.Data;
          }
        }
        onApprovalNeeded(taskId, lastEvent.Message || "human_approval", ctxString);
      }
    } else if (lastEvent.Kind === 'await_clarification') {
      onApprovalNeeded(taskId, "clarification", lastEvent.Message);
    }
  }, [events, taskId, onApprovalNeeded]);

  const renderEvent = (evt, idx) => {
    // Special rendering for ReAct reasoning
    if (evt.Kind === 'ReAct Thought') {
      return (
        <motion.div 
          key={`${evt.Time}-${idx}`}
          initial={{ opacity: 0, scale: 0.95, y: 10 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          whileHover={{ scale: 1.02 }}
          transition={{ duration: 0.3, type: "spring" }}
          layout
          className="timeline-event react-card"
        >
          <div className="react-icon">
            <BrainCircuit size={20} />
          </div>
          <div className="react-content">
            <div className="react-header">Agent Reasoning</div>
            <div className="react-thought">{evt.Message}</div>
          </div>
        </motion.div>
      );
    }
    
    // Special rendering for Tool execution
    if (evt.Kind.startsWith('tool ')) {
      return (
        <motion.div 
          key={`${evt.Time}-${idx}`}
          initial={{ opacity: 0, x: 20, y: 10 }}
          animate={{ opacity: 1, x: 0, y: 0 }}
          whileHover={{ scale: 1.02 }}
          transition={{ duration: 0.3, type: "spring" }}
          layout
          className="timeline-event tool-card"
        >
          <div className="tool-icon">
            <Wrench size={16} />
          </div>
          <div className="tool-content">
            <div className="tool-name">{evt.Kind}</div>
            <div className="tool-result">{evt.Message}</div>
          </div>
        </motion.div>
      );
    }

    // Default rendering
    return (
      <motion.div 
        key={`${evt.Time}-${idx}`}
        initial={{ opacity: 0, x: -20, y: 10 }}
        animate={{ opacity: 1, x: 0, y: 0 }}
        whileHover={{ x: 5 }}
        transition={{ duration: 0.3, type: "spring" }}
        layout
        className="timeline-event"
      >
        <div className="timeline-dot" />
        <div className="event-content">
          <div className="event-header">
            <span className="event-agent">{evt.Agent}</span>
            <span className="event-time">{format(new Date(evt.Time), 'HH:mm:ss.SSS')}</span>
          </div>
          <span className="event-kind">{evt.Kind}</span>
          <div className="event-message">{evt.Message}</div>
          {evt.Duration && (
            <div className="event-duration">{evt.Duration}</div>
          )}
        </div>
      </motion.div>
    );
  };

  return (
    <div style={{ paddingBottom: '2rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
        <h2 style={{ fontSize: '1.25rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          Pipeline Execution
          <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>{taskId.substring(0,8)}</span>
        </h2>
        
        {status !== 'pending' && (
          <span className={`status-badge status-${status}`}>
            {status.toUpperCase()}
          </span>
        )}
      </div>

      <div className="timeline">
        {events.find(e => e.Kind === 'dag_structure') ? (
          <div style={{ marginBottom: '2rem' }}>
            <DAGVisualizer 
              pipelineSteps={JSON.parse(events.find(e => e.Kind === 'dag_structure').Message)} 
              taskEvents={events.map(e => ({ action: e.Kind, status: e.Message, component: e.Agent, duration: e.Duration }))}
            />
          </div>
        ) : (
          <AnimatePresence>
            {events.map((evt, idx) => renderEvent(evt, idx))}
          </AnimatePresence>
        )}
        {status === 'pending' && events.length > 0 && (
          <motion.div 
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="timeline-event"
            style={{ paddingBottom: 0 }}
          >
            <div className="timeline-dot" style={{ animation: 'pulse 1.5s infinite' }} />
            <div className="event-content" style={{ color: 'var(--text-muted)', fontStyle: 'italic', marginTop: '0' }}>
              Working...
            </div>
          </motion.div>
        )}
      </div>

      {finalData && (
        <motion.div 
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          className="glass-card" 
          style={{ marginTop: '2rem' }}
        >
          <h3 style={{ marginBottom: '1rem', color: status === 'error' ? '#fecaca' : '#bbf7d0', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            {status === 'error' ? <><AlertTriangle size={20} /> Execution Failed</> : <><CheckCircle size={20} /> Final Result</>}
          </h3>
          <pre style={{ 
            background: 'rgba(0,0,0,0.3)', 
            padding: '1rem', 
            borderRadius: 'var(--radius-sm)',
            overflowX: 'auto',
            fontSize: '0.85rem',
            color: 'var(--text-secondary)',
            whiteSpace: 'pre-wrap'
          }}>
            {JSON.stringify(finalData, null, 2)}
          </pre>
        </motion.div>
      )}
    </div>
  );
};

export default Timeline;
