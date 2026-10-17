import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { format } from 'date-fns';
import { Activity, BrainCircuit, Wrench, CheckCircle, AlertTriangle, Copy, ShieldAlert } from 'lucide-react';
import DAGVisualizer from './DAGVisualizer';
import { apiFetch } from '../utils/api';

const Timeline = ({ taskId, onApprovalNeeded }) => {
  const [events, setEvents] = useState([]);
  const [status, setStatus] = useState('pending'); // pending, ok, error
  const [finalData, setFinalData] = useState(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!taskId) return;

    setEvents([]);
    setStatus('pending');
    setFinalData(null);
    setCopied(false);

    const eventController = new AbortController();
    const handleEventMessage = (raw) => {
      try {
        const msg = JSON.parse(raw);
        if (msg.Type !== 'agent_event') return;
        const payload = msg.Payload;

        // Only process events for this task
        if (payload.task_id !== taskId) return;

        // Map the new payload format to the structure expected by the Timeline component
        const ev = {
          TaskID: payload.task_id,
          Time: new Date().toISOString(),
          Agent: payload.agent,
          Kind: payload.kind,
          Message: payload.message,
          Duration: payload.duration,
          Data: payload.data
        };

        setEvents((prev) => [...prev, ev]);
      } catch (err) {
        console.error("Error parsing event", err);
      }
    };

    const readEventStream = async () => {
      const endpoint = `/api/v1/events/stream?task_id=${encodeURIComponent(taskId)}`;
      while (!eventController.signal.aborted) {
        try {
          const response = await apiFetch(endpoint, { signal: eventController.signal });
          if ([401, 403, 404].includes(response.status)) {
            console.error(`Event stream denied (${response.status})`);
            return;
          }
          if (!response.ok || !response.body) {
            throw new Error(`Event stream failed (${response.status})`);
          }

          const reader = response.body.getReader();
          const decoder = new TextDecoder();
          let buffer = '';
          while (!eventController.signal.aborted) {
            const { value, done } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
            let boundary;
            while ((boundary = buffer.indexOf('\n\n')) >= 0) {
              const frame = buffer.slice(0, boundary);
              buffer = buffer.slice(boundary + 2);
              const data = frame.split('\n')
                .filter(line => line.startsWith('data:'))
                .map(line => line.slice(5).trimStart())
                .join('\n');
              if (data) handleEventMessage(data);
            }
          }
          if (!eventController.signal.aborted) await new Promise(resolve => setTimeout(resolve, 1000));
        } catch (err) {
          if (eventController.signal.aborted) return;
          console.error("Event stream error", err);
          await new Promise(resolve => setTimeout(resolve, 1000));
        }
      }
    };
    readEventStream();

    const statusPollingInterval = setInterval(async () => {
      try {
        const tskRes = await apiFetch(`/task?id=${taskId}`);
        if (tskRes.ok) {
          const data = await tskRes.json();
          if (data.status !== 'pending' && data.status !== 'await_human' && data.status !== 'await_clarification') {
            setStatus(data.status);
            if (data.data) setFinalData(data.data);
            if (data.error) setFinalData({ error: data.error });

            // The Analyst stores the one-shot final result with status
            // "completed". Stop polling immediately or the next request
            // falls back to the older persisted state and hides callback data.
            if (['ok', 'success', 'completed', 'error', 'governance_blocked'].includes(data.status)) {
              clearInterval(statusPollingInterval);
              setTimeout(() => eventController.abort(), 1000);
            }

            // Reconstruct historical events if this is an old task and events is empty
            {
              const histEvents = [];
              if (data.completed_steps && Array.isArray(data.completed_steps) && data.completed_steps.length > 0) {
                 data.completed_steps.forEach(stepName => {
                   const result = data.data && data.data[stepName] ? data.data[stepName] : 'Completed';
                   histEvents.push({ TaskID: taskId, Time: new Date().toISOString(), Agent: 'System', Kind: 'tool ' + stepName, Message: typeof result === 'string' ? result : JSON.stringify(result) });
                 });
              } else if (data.react_history && Array.isArray(data.react_history) && data.react_history.length > 0) {
                data.react_history.forEach(react => {
                  if (react.llm_stats) {
                    histEvents.push({
                      TaskID: taskId,
                      Time: new Date().toISOString(),
                      Agent: 'Ollama',
                      Kind: 'llm_stats',
                      Message: `${Number(react.llm_stats.output_tokens_per_second || 0).toFixed(1)} tok/s`,
                      Duration: `${Number(react.llm_stats.total_duration_seconds || 0).toFixed(2)}s`,
                      Data: JSON.stringify(react.llm_stats)
                    });
                  }
                  if (react.thought) {
                    histEvents.push({ TaskID: taskId, Time: new Date().toISOString(), Agent: 'System', Kind: 'ReAct Thought', Message: react.thought });
                  }
                  if (react.action) {
                    histEvents.push({ TaskID: taskId, Time: new Date().toISOString(), Agent: 'System', Kind: 'tool ' + react.action, Message: react.action_input });
                  }
                  if (react.observation) {
                    histEvents.push({ TaskID: taskId, Time: new Date().toISOString(), Agent: 'System', Kind: 'tool result', Message: react.observation });
                  }
                });
              }
              if (histEvents.length > 0) {
                setEvents(prev => prev.length === 0 ? histEvents : prev);
              }
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
      eventController.abort();
      clearInterval(statusPollingInterval);
    };
  }, [taskId]);

  // A governance verdict is a policy outcome, not a technical failure: show it differently.
  const governanceBlocked = status === 'governance_blocked' && finalData && finalData.decision;
  const needsApproval = governanceBlocked && finalData.decision === 'APPROVAL_REQUIRED';
  const headerColor = governanceBlocked
    ? (needsApproval ? '#f59e0b' : '#f87171')
    : status === 'error' ? '#f87171' : 'var(--secondary)';

  const copyFinalResult = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(finalData, null, 2));
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch (error) {
      console.error('Could not copy final result', error);
    }
  };

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
          } catch {
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
    if (evt.Kind === 'llm_stats') {
      let stats = {};
      try {
        stats = typeof evt.Data === 'string' ? JSON.parse(evt.Data) : (evt.Data || {});
      } catch {
        stats = {};
      }
      const number = (value, digits = 1) => Number(value || 0).toFixed(digits);
      return (
        <motion.div
          key={`${evt.Time}-${idx}`}
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25 }}
          layout
          className="timeline-event llm-stats-card"
        >
          <div className="llm-stats-icon"><Activity size={16} /></div>
          <div className="llm-stats-content">
            <div className="llm-stats-heading">
              <span>{stats.model || 'Ollama'}</span>
              <strong>{number(stats.output_tokens_per_second)} tok/s</strong>
            </div>
            <div className="llm-stats-grid">
              <span><b>{stats.prompt_tokens || 0}</b> prompt tokens</span>
              <span><b>{number(stats.prompt_tokens_per_second)}</b> prompt tok/s</span>
              <span><b>{stats.output_tokens || 0}</b> output tokens</span>
              <span><b>{number(stats.total_duration_seconds, 2)}s</b> total</span>
              <span><b>{number(stats.load_duration_seconds, 2)}s</b> load</span>
            </div>
          </div>
        </motion.div>
      );
    }

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
        {status === 'pending' && (
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
          <div style={{ marginBottom: '1rem', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem' }}>
            <h3 style={{ margin: 0, color: headerColor, display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              {governanceBlocked
                ? <><ShieldAlert size={20} /> {needsApproval ? 'Blocked by Prudence: approval required' : 'Denied by Prudence'}</>
                : status === 'error'
                  ? <><AlertTriangle size={20} /> Execution Failed</>
                  : <><CheckCircle size={20} /> Final Result</>}
            </h3>
            <button
              type="button"
              className="btn btn-secondary"
              onClick={copyFinalResult}
              aria-label="Copiar resultado final"
              style={{ padding: '0.4rem 0.7rem', display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.8rem' }}
            >
              <Copy size={15} /> {copied ? 'Copiado' : 'Copiar'}
            </button>
          </div>
          {governanceBlocked && (
            <div style={{ marginBottom: '1rem', padding: '0.85rem 1rem', borderRadius: 'var(--radius-md)', border: `1px solid ${headerColor}`, color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.5 }}>
              <div><strong>Decision:</strong> {finalData.decision}{finalData.reason ? ` · ${finalData.reason}` : ''}</div>
              {finalData.approver && <div><strong>Approver:</strong> {finalData.approver}</div>}
              <div><strong>AI system:</strong> {finalData.aiSystemId || '—'} · <strong>Pipeline:</strong> {finalData.pipeline}</div>
              <div style={{ marginTop: '0.4rem', color: 'var(--text-muted)' }}>
                {needsApproval
                  ? 'AOS did not run any step. Approve the AI system in Prudence, then run the request again.'
                  : 'AOS did not run any step. The governance decision is taken in Prudence, not in AOS.'}
              </div>
            </div>
          )}
          <pre style={{
            background: 'color-mix(in srgb, var(--bg-base) 80%, transparent)',
            padding: '1.25rem',
            borderRadius: 'var(--radius-md)',
            overflowX: 'auto',
            fontSize: '0.85rem',
            color: 'var(--text-secondary)',
            whiteSpace: 'pre-wrap',
            border: '1px solid var(--border-light)'
          }}>
            {JSON.stringify(finalData, null, 2)}
          </pre>
        </motion.div>
      )}
    </div>
  );
};

export default Timeline;
