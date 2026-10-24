import React, { useState, useEffect } from 'react';
import { Clock, XCircle, Play, Eye, RefreshCw } from 'lucide-react';
import { Alert, Button, Card, EmptyState, Spinner } from '@chestertown/design';
import { apiFetch } from '../utils/api';
import { ernestEvidenceUrl } from '../utils/chestertown';

export default function HistoryPanel({ onSelectTask }) {
  const [tasks, setTasks] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [rerunning, setRerunning] = useState(null);
  const [runError, setRunError] = useState(null);

  useEffect(() => {
    fetchTasks();
  }, []);

  const fetchTasks = async () => {
    try {
      setLoading(true);
      setError(null);
      const res = await apiFetch('/api/v1/tasks');
      if (!res.ok) throw new Error('Failed to fetch task history');
      const data = await res.json();
      setTasks(data || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  const rerunTask = async (task) => {
    if (rerunning) return;
    if (!window.confirm('Run this operation again? This creates a new execution and may repeat its actions.')) return;
    setRerunning(task.id);
    setRunError(null);
    try {
      const response = await apiFetch('/ask_structured', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ operation: task.intent, params: task.params || {}, async: true })
      });
      if (!response.ok) throw new Error(`Could not start execution (${response.status})`);
      const data = await response.json();
      if (!data.id || data.error) throw new Error(data.error || 'Missing execution ID');
      onSelectTask(data.id);
    } catch (err) {
      setRunError(err.message);
    } finally {
      setRerunning(null);
    }
  };

  const getStatusIcon = (task) => {
    if (task.error) return <XCircle size={18} style={{ color: 'var(--danger)' }} />;
    if (task.missing_params && task.missing_params.length > 0) {
      return <Clock size={18} className="text-accent" />; // Clarification pending
    }
    return <Play size={18} className="text-secondary" />; // In progress
  };

  const refresh = (
    <Button size="sm" onClick={fetchTasks} disabled={loading}>
      <RefreshCw size={14} /> Refresh
    </Button>
  );

  return (
    <div className="settings-container" style={{ width: '100%' }}>
      <Card title={<span className="ct-row"><Clock size={20} /> Task execution history</span>} actions={refresh}>
        {runError && <Alert tone="danger" className="ct-mb-4">{runError}</Alert>}
        {loading ? (
          <Spinner label="Loading task history…" />
        ) : error ? (
          <Alert tone="danger" title="Could not load the history">{error}</Alert>
        ) : tasks.length === 0 ? (
          <EmptyState title="No historical tasks found.">Run something from the chat and it will appear here.</EmptyState>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--sp-3)' }}>
            {tasks.map(task => (
              <Card key={task.id} className="history-item" onClick={() => onSelectTask(task.id)} style={{ cursor: 'pointer' }}>
                <div className="ct-row ct-between" style={{ alignItems: 'flex-start' }}>
                  <div style={{ minWidth: 0 }}>
                    <div className="ct-row" style={{ gap: 'var(--sp-2)' }}>
                      {getStatusIcon(task)}
                      <h3 className="ct-mt-0 ct-mb-0 ct-text-lg">{task.intent || 'Unknown intent'}</h3>
                    </div>
                    <div className="ct-muted ct-text-sm ct-mt-4">
                      Task ID: <code>{task.id.split('-')[0]}…</code> · Pipeline: {task.pipeline || 'None'}
                      {task.ai_system_id && (
                        <>
                          {' · '}
                          <a
                            href={ernestEvidenceUrl(task.ai_system_id)}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={(e) => e.stopPropagation()}
                            title="This AI system's evidence in Ernest"
                          >
                            evidence ↗
                          </a>
                        </>
                      )}
                    </div>
                  </div>

                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 'var(--sp-2)' }}>
                    <div className="ct-row" style={{ gap: 'var(--sp-2)' }}>
                      <Button size="sm" onClick={(e) => { e.stopPropagation(); onSelectTask(task.id); }}>
                        <Eye size={14} /> View details
                      </Button>
                      <Button
                        size="sm"
                        variant="primary"
                        disabled={!!rerunning || !task.intent}
                        onClick={(e) => { e.stopPropagation(); rerunTask(task); }}
                      >
                        <Play size={14} /> {rerunning === task.id ? 'Starting…' : 'Re-run'}
                      </Button>
                    </div>
                    <span className="ct-muted ct-text-sm">{new Date(task.created_at).toLocaleString()}</span>
                    <span className="ct-text-sm">{task.completed_steps ? task.completed_steps.length : 0} steps completed</span>
                  </div>
                </div>
              </Card>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
