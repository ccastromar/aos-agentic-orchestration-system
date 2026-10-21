import React, { useState, useEffect } from 'react';
import { BrainCircuit, Trash2, Loader2, RefreshCw } from 'lucide-react';
import { Alert, Button, Card, EmptyState, Spinner } from '@chestertown/design';
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
    <Card
      title={<span className="ct-row"><BrainCircuit size={20} /> Memory management</span>}
      actions={
        <Button size="sm" onClick={fetchMemories} disabled={loading}>
          <RefreshCw size={14} className={loading ? 'spin' : ''} /> Refresh
        </Button>
      }
    >
      {error && <Alert tone="danger" className="ct-mb-4">{error}</Alert>}

      {loading ? (
        <Spinner label="Loading memories…" />
      ) : error ? null : memories.length === 0 ? (
        <EmptyState title="No personal facts remembered yet.">
          Try telling the assistant something about yourself!
        </EmptyState>
      ) : (
        <div style={{ display: 'grid', gap: 'var(--sp-4)', gridTemplateColumns: 'repeat(auto-fill, minmax(min(300px, 100%), 1fr))' }}>
          {memories.map(mem => (
            <Card key={mem.id}>
              <p className="ct-mt-0">{mem.text}</p>
              <p className="ct-muted ct-text-sm">{new Date(mem.timestamp).toLocaleString()}</p>
              <div className="ct-row" style={{ justifyContent: 'flex-end' }}>
                <Button size="sm" variant="danger" onClick={() => handleDelete(mem.id)} disabled={deletingId === mem.id}>
                  {deletingId === mem.id ? <Loader2 size={14} className="spin" /> : <Trash2 size={14} />}
                  Forget
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}
    </Card>
  );
}
