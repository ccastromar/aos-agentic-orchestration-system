import React, { useState, useEffect } from 'react';
import { Clock, XCircle, Search, Play, Eye } from 'lucide-react';
import { apiFetch } from '../utils/api';

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
    if (task.error) return <XCircle size={18} style={{ color: "#ef4444" }} />;
    if (task.missing_params && task.missing_params.length > 0) {
      return <Clock size={18} className="text-accent" />; // Clarification pending
    }
    return <Play size={18} className="text-secondary" />; // In progress
  };

  if (loading) {
    return (
      <div className="settings-container" style={{ textAlign: 'center', padding: '3rem' }}>
        <p>Loading task history...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="settings-container" style={{ padding: '2rem' }}>
        <div className="glass-panel" style={{ padding: '2rem', border: '1px solid #ef4444' }}>
          <h3 style={{ color: '#ef4444', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <XCircle /> Error
          </h3>
          <p>{error}</p>
          <button className="btn btn-secondary mt-4" onClick={fetchTasks}>Retry</button>
        </div>
      </div>
    );
  }

  return (
    <div className="settings-container" style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: '2rem' }}>
      <section className="glass-panel" style={{ padding: '2rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem', borderBottom: '1px solid var(--border-light)', paddingBottom: '1rem' }}>
          <h2 style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', margin: 0 }}>
            <Clock className="text-primary" /> Task Execution History
          </h2>
          <button className="btn btn-secondary" onClick={fetchTasks} style={{ padding: '0.4rem 1rem', fontSize: '0.8rem' }}>
            Refresh
          </button>
        </div>

        {runError && <p role="alert" style={{ color: "#ef4444" }}>{runError}</p>}
        {tasks.length === 0 ? (
          <div style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-muted)' }}>
            <Search size={48} opacity={0.2} style={{ margin: '0 auto 1rem' }} />
            <p>No historical tasks found.</p>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            {tasks.map(task => (
              <div
                key={task.id}
                className="glass-panel"
                style={{
                  padding: '1.5rem',
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  background: 'var(--bg-surface-hover)',
                  cursor: 'pointer',
                  transition: 'transform 0.2s ease, box-shadow 0.2s ease'
                }}
                onClick={() => onSelectTask(task.id)}
                onMouseEnter={(e) => e.currentTarget.style.transform = 'translateY(-2px)'}
                onMouseLeave={(e) => e.currentTarget.style.transform = 'translateY(0)'}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                    {getStatusIcon(task)}
                    <h3 style={{ fontSize: '1.1rem', margin: 0, color: 'var(--text-primary)' }}>
                      {task.intent || 'Unknown Intent'}
                    </h3>
                  </div>
                  <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                    Task ID: <span style={{ fontFamily: 'monospace' }}>{task.id.split('-')[0]}...</span> •
                    Pipeline: <span style={{ color: 'var(--secondary)' }}>{task.pipeline || 'None'}</span>
                  </div>
                </div>

                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '0.8rem' }}>
                  <div style={{ display: 'flex', gap: '0.5rem' }}>
                    <button
                      className="btn btn-secondary"
                      onClick={(e) => { e.stopPropagation(); onSelectTask(task.id); }}
                      style={{ padding: '0.3rem 0.8rem', fontSize: '0.8rem', display: 'flex', alignItems: 'center', gap: '0.3rem' }}
                    >
                      <Eye size={14} /> View Details
                    </button>
                    <button
                      className="btn btn-primary"
                      disabled={!!rerunning || !task.intent}
                      onClick={(e) => { e.stopPropagation(); rerunTask(task); }}
                      style={{ padding: '0.3rem 0.8rem', fontSize: '0.8rem', display: 'flex', alignItems: 'center', gap: '0.3rem' }}
                    >
                      <Play size={14} /> {rerunning === task.id ? "Starting…" : "Re-run"}
                    </button>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '0.2rem' }}>
                    <span style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                      {new Date(task.created_at).toLocaleString()}
                    </span>
                    <div style={{ fontSize: '0.8rem' }}>
                      {task.completed_steps ? task.completed_steps.length : 0} steps completed
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
