import React, { useState, useEffect } from 'react';
import { BrainCircuit, Trash2, Loader2, RefreshCw } from 'lucide-react';
import { apiFetch } from '../utils/api';

export default function MemorySettings() {
  const [memories, setMemories] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [deletingId, setDeletingId] = useState(null);

  const fetchMemories = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await apiFetch('/api/v1/memories');
      if (!res.ok) {
        throw new Error('Failed to fetch memories');
      }
      const data = await res.json();
      setMemories(data || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchMemories();
  }, []);

  const handleDelete = async (id) => {
    if (!window.confirm("Are you sure you want to forget this fact?")) return;

    setDeletingId(id);
    try {
      const res = await apiFetch(`/api/v1/memories?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
      if (!res.ok) {
        throw new Error('Failed to delete memory');
      }
      setMemories(prev => prev.filter(m => m.id !== id));
    } catch (err) {
      alert("Error deleting memory: " + err.message);
    } finally {
      setDeletingId(null);
    }
  };

  return (
    <section className="glass-panel" style={{ padding: '2rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem', borderBottom: '1px solid var(--border-light)', paddingBottom: '1rem' }}>
        <h2 style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', margin: 0 }}>
          <BrainCircuit className="text-primary" /> Memory Management
        </h2>
        <button onClick={fetchMemories} className="btn btn-secondary" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 1rem' }} disabled={loading}>
          <RefreshCw size={16} className={loading ? "spin" : ""} /> Refresh
        </button>
      </div>

      {error && (
        <div style={{ padding: '1rem', background: 'rgba(239, 68, 68, 0.1)', color: 'rgb(239, 68, 68)', borderRadius: 'var(--radius-md)', marginBottom: '1rem' }}>
          {error}
        </div>
      )}

      {loading ? (
        <div style={{ display: 'flex', justifyContent: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
          <Loader2 className="spin" size={24} />
        </div>
      ) : memories.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-muted)', background: 'rgba(0,0,0,0.1)', borderRadius: 'var(--radius-md)' }}>
          <BrainCircuit size={48} style={{ opacity: 0.2, margin: '0 auto 1rem' }} />
          <p>No personal facts remembered yet.</p>
          <small>Try telling the assistant something about yourself!</small>
        </div>
      ) : (
        <div style={{ display: 'grid', gap: '1rem', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))' }}>
          {memories.map(mem => (
            <div key={mem.id} style={{
              padding: '1.25rem',
              background: 'var(--bg-secondary)',
              borderRadius: 'var(--radius-md)',
              border: '1px solid var(--border-light)',
              display: 'flex',
              flexDirection: 'column',
              justifyContent: 'space-between',
              gap: '1rem'
            }}>
              <div>
                <p style={{ margin: 0, fontSize: '1.05rem', lineHeight: '1.5' }}>{mem.text}</p>
                <small style={{ color: 'var(--text-muted)', display: 'block', marginTop: '0.5rem' }}>
                  {new Date(mem.timestamp).toLocaleString()}
                </small>
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                <button
                  className="btn btn-secondary"
                  onClick={() => handleDelete(mem.id)}
                  disabled={deletingId === mem.id}
                  style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.4rem 0.8rem', color: 'rgb(239, 68, 68)', borderColor: 'rgba(239, 68, 68, 0.3)' }}
                >
                  {deletingId === mem.id ? <Loader2 size={16} className="spin" /> : <Trash2 size={16} />}
                  Forget
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
