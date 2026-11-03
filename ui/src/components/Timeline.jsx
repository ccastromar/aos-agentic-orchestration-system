import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { format } from 'date-fns';
import { Wrench, CheckCircle, AlertTriangle, Copy, ShieldAlert } from 'lucide-react';
import DAGVisualizer from './DAGVisualizer';
import { apiFetch, getAuth, UNAUTHORIZED_EVENT } from '../utils/api';
import { devLoginEnabled, ssoEnabled } from '../utils/oidc';
import { ernestEvidenceUrl, prudenceSystemUrl } from '../utils/chestertown';
import { Button } from '@chestertown/design';

// The pipeline context shown in the approval modal, without the bookkeeping keys of each step.
const approvalContext = (ctxData) => {
  const filtered = Object.entries(ctxData)
    .filter(([k]) => !k.includes('._') && !k.includes('.statusCode') && !k.includes('.message') && !k.includes('.ok') && !k.includes('.success') && !k.includes('.status'))
    .map(([k,v]) => {
      const shortKey = k.split('.').pop();
      return `- ${shortKey}: ${JSON.stringify(v)}`;
    });
  return "Pipeline Context: \n" + filtered.join("\n");
};

const Timeline = ({ taskId, onApprovalNeeded }) => {
  const [events, setEvents] = useState([]);
  // loading = nothing heard from the server yet; pending = the task is still running;
  // unavailable = AOS refused the task (absent, or not visible to this identity).
  const [status, setStatus] = useState('loading');
  const [finalData, setFinalData] = useState(null);
  const [copied, setCopied] = useState(false);
  // The Prudence AI system and decision a governed run was evaluated under (from /task).
  const [governance, setGovernance] = useState(null);

  useEffect(() => {
    if (!taskId) return;

    setEvents([]);
    setStatus('loading');
    setFinalData(null);
    setCopied(false);
    setGovernance(null);

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

    // Ask for the task right away: a finished task (opened from History) must not sit behind a
    // "Working..." indicator for the first polling interval, which looked like a re-run.
    let statusPollingInterval = null;
    let stopped = false;
    let decisionAsked = false;
    const pollStatus = async () => {
      if (stopped) return;
      try {
        const tskRes = await apiFetch(`/task?id=${taskId}`);
        // AOS answers 404 for a task this identity cannot see (anonymous included), so it does
        // not reveal that the task exists. Stop here instead of polling behind "Loading task…".
        if ([401, 403, 404].includes(tskRes.status)) {
          stopped = true;
          clearInterval(statusPollingInterval);
          eventController.abort();
          setStatus('unavailable');
          return;
        }
        if (tskRes.ok) {
          const data = await tskRes.json();
          if (data.status !== 'pending' && data.status !== 'await_human' && data.status !== 'await_clarification') {
            setStatus(data.status);
            if (data.ai_system_id) setGovernance({ aiSystemId: data.ai_system_id, decisionId: data.decision_id });
            if (data.data) setFinalData(data.data);
            if (data.error) setFinalData({ error: data.error });

            // The Analyst stores the one-shot final result with status
            // "completed". Stop polling immediately or the next request
            // falls back to the older persisted state and hides callback data.
            if (['ok', 'success', 'completed', 'error', 'governance_blocked', 'rejected'].includes(data.status)) {
              stopped = true;
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
              }
              if (histEvents.length > 0) {
                setEvents(prev => prev.length === 0 ? histEvents : prev);
              }
            }
          } else if (data.status === 'await_human' || data.status === 'await_clarification') {
            setStatus(data.status);
            setFinalData(null); // Clear final data if we are awaiting human input
            // A task opened after it stopped (a link, History) never sees the live await_* event,
            // so open the decision from the stored task too, once. Without the real gate AOS answers
            // 409. /task carries it; older AOS builds do not, and then the task listing does.
            if (!decisionAsked) {
              let listed = null;
              if (!data.gate) {
                const listRes = await apiFetch('/api/v1/tasks');
                listed = listRes.ok ? (await listRes.json()).find(t => t.id === taskId) : null;
              }
              const gate = data.gate || listed?.gate;
              const params = data.params || listed?.params;
              const missingParams = data.missing_params || listed?.missing_params;
              if (data.status === 'await_clarification') {
                decisionAsked = true;
                const missing = Array.isArray(missingParams) && missingParams.length
                  ? `Missing parameters: ${missingParams.join(', ')}`
                  : 'Please provide the missing parameters.';
                onApprovalNeeded(taskId, 'clarification', missing);
              } else if (gate) {
                decisionAsked = true;
                onApprovalNeeded(taskId, gate, params ? approvalContext(params) : 'Please review and approve this action.');
              }
            }
          } else {
            setStatus('pending'); // still running
          }
        }
      } catch (e) {
        console.error("Status polling error", e);
      }
    };
    statusPollingInterval = setInterval(pollStatus, 3000);
    pollStatus();

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
    : status === 'error' || status === 'rejected' ? '#f87171' : 'var(--secondary)';

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
            ctxString = approvalContext(JSON.parse(lastEvent.Data));
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

        {status !== 'pending' && status !== 'loading' && status !== 'unavailable' && (
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
        {(status === 'pending' || status === 'loading') && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="timeline-event"
            style={{ paddingBottom: 0 }}
          >
            <div className="timeline-dot" style={{ animation: 'pulse 1.5s infinite' }} />
            <div className="event-content" style={{ color: 'var(--text-muted)', fontStyle: 'italic', marginTop: '0' }}>
              {status === 'loading' ? 'Loading task…' : 'Working...'}
            </div>
          </motion.div>
        )}
        {status === 'unavailable' && (
          <div className="glass-card" style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: '0.75rem' }}>
            <h3 style={{ margin: 0, display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <AlertTriangle size={20} /> Task not available
            </h3>
            {getAuth() ? (
              <p style={{ margin: 0, color: 'var(--text-muted)' }}>
                This task does not exist, or your account cannot see it. Its owner, or an overseer
                signed in as themselves, can open it.
              </p>
            ) : (
              <>
                <p style={{ margin: 0, color: 'var(--text-muted)' }}>
                  This task does not exist, or you need to sign in to see it.
                </p>
                {(ssoEnabled || devLoginEnabled) && (
                  <Button onClick={() => window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT))}>
                    Sign in
                  </Button>
                )}
              </>
            )}
          </div>
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
                  : status === 'rejected'
                  ? <><AlertTriangle size={20} /> Rejected{finalData?.decided_by ? ` by ${finalData.decided_by}` : ''}</>
                  : <><CheckCircle size={20} /> Final Result</>}
            </h3>
            <Button size="sm" onClick={copyFinalResult} aria-label="Copy the final result">
              <Copy size={14} /> {copied ? 'Copied' : 'Copy'}
            </Button>
          </div>
          {!governanceBlocked && governance && (
            <div style={{ marginBottom: '1rem', padding: '0.6rem 1rem', borderRadius: 'var(--radius-md)', border: '1px solid var(--border-light)', color: 'var(--text-secondary)', fontSize: '0.85rem', display: 'flex', flexWrap: 'wrap', gap: '0.35rem 1rem', alignItems: 'baseline' }}>
              <span>
                <strong>Governed by Prudence</strong> · AI system {governance.aiSystemId}
                {governance.decisionId && <span style={{ color: 'var(--text-muted)' }}> · decision <code>{governance.decisionId.substring(0, 8)}</code></span>}
              </span>
              <span style={{ marginLeft: 'auto', display: 'flex', gap: '1rem' }}>
                <a href={prudenceSystemUrl(governance.aiSystemId)} target="_blank" rel="noopener noreferrer">Open in Prudence ↗</a>
                <a href={ernestEvidenceUrl(governance.aiSystemId)} target="_blank" rel="noopener noreferrer">Evidence in Ernest ↗</a>
              </span>
            </div>
          )}
          {governanceBlocked && (
            <div style={{ marginBottom: '1rem', padding: '0.85rem 1rem', borderRadius: 'var(--radius-md)', border: `1px solid ${headerColor}`, color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.5 }}>
              <div><strong>Decision:</strong> {finalData.decision}{finalData.reason ? ` · ${finalData.reason}` : ''}</div>
              {finalData.approver && <div><strong>Approver:</strong> {finalData.approver}</div>}
              <div>
                <strong>AI system:</strong> {finalData.aiSystemId || '—'}
                {finalData.aiSystemId && (
                  <>
                    {' '}
                    <a href={ernestEvidenceUrl(finalData.aiSystemId)} target="_blank" rel="noopener noreferrer" style={{ color: 'var(--text-muted)' }}>
                      evidence ↗
                    </a>
                  </>
                )}
                {' · '}<strong>Pipeline:</strong> {finalData.pipeline}
              </div>
              <div style={{ marginTop: '0.4rem', color: 'var(--text-muted)' }}>
                {needsApproval
                  ? 'AOS did not run any step. Approve the AI system in Prudence, then run the request again.'
                  : 'AOS did not run any step. The governance decision is taken in Prudence, not in AOS.'}
                {finalData.aiSystemId && (
                  <>
                    {' '}
                    <a href={prudenceSystemUrl(finalData.aiSystemId)} target="_blank" rel="noopener noreferrer">
                      Open in Prudence ↗
                    </a>
                  </>
                )}
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
