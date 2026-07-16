import React, { useState } from 'react';
import { motion } from 'framer-motion';
import { AlertTriangle, Check, X, Loader2 } from 'lucide-react';

const HumanApprovalModal = ({ taskId, gate, message, onResolved }) => {
  const [isLoading, setIsLoading] = useState(false);
  const [action, setAction] = useState(null);

  const handleDecision = async (approved) => {
    setIsLoading(true);
    setAction(approved ? 'approve' : 'reject');
    
    try {
      const endpoint = approved ? 'task/approve' : 'task/reject';
      // URL: POST /task/approve?id=...&gate=...
      const response = await fetch(`/${endpoint}?id=${taskId}&gate=${gate}`, {
        method: 'POST'
      });

      if (!response.ok) {
        throw new Error('Failed to submit decision');
      }
      
      onResolved();
    } catch (err) {
      console.error(err);
      setIsLoading(false);
      setAction(null);
    }
  };

  return (
    <motion.div 
      className="modal-overlay"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      <motion.div 
        initial={{ opacity: 0, scale: 0.9, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.9, y: 20 }}
        transition={{ type: "spring", bounce: 0.4 }}
        className="glass-panel modal-content"
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '1.5rem', color: 'var(--accent)' }}>
          <AlertTriangle size={32} />
          <h2 style={{ color: 'var(--text-primary)', margin: 0 }}>Human Approval Required</h2>
        </div>
        
        <p style={{ color: 'var(--text-secondary)', marginBottom: '1rem' }}>
          The agent pipeline has paused execution because a tool requires explicit human authorization.
        </p>

        {message && (
          <div style={{ 
            background: 'color-mix(in srgb, var(--accent) 10%, transparent)', 
            borderLeft: '4px solid var(--accent)', 
            padding: '1rem', 
            borderRadius: '0 var(--radius-sm) var(--radius-sm) 0',
            marginBottom: '1.5rem',
            color: 'var(--text-primary)',
            fontSize: '0.95rem',
            lineHeight: '1.5',
            whiteSpace: 'pre-wrap',
            maxHeight: '250px',
            overflowY: 'auto'
          }}>
            <strong>Action Context:</strong><br/>
            {message}
          </div>
        )}
        
        <div style={{ background: 'color-mix(in srgb, var(--bg-base) 80%, transparent)', padding: '1rem', borderRadius: 'var(--radius-sm)', marginBottom: '2rem', border: '1px solid var(--border-light)' }}>
          <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>Task ID</div>
          <div style={{ fontFamily: 'monospace', marginBottom: '0.5rem' }}>{taskId}</div>
          <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>Gate Name</div>
          <div style={{ fontFamily: 'monospace', color: 'var(--accent)' }}>{gate}</div>
        </div>

        <div className="modal-actions">
          <button 
            className="btn-secondary"
            onClick={() => handleDecision(false)}
            disabled={isLoading}
            style={{ borderColor: 'rgba(239, 68, 68, 0.5)', color: '#fca5a5' }}
          >
            {isLoading && action === 'reject' ? <Loader2 size={18} className="animate-spin" /> : <X size={18} />}
            Reject
          </button>
          
          <button 
            className="btn-primary"
            onClick={() => handleDecision(true)}
            disabled={isLoading}
            style={{ background: 'linear-gradient(135deg, #22c55e, #16a34a)', boxShadow: '0 4px 14px 0 rgba(34, 197, 94, 0.39)' }}
          >
            {isLoading && action === 'approve' ? <Loader2 size={18} className="animate-spin" /> : <Check size={18} />}
            Approve Action
          </button>
        </div>
      </motion.div>
    </motion.div>
  );
};

export default HumanApprovalModal;
