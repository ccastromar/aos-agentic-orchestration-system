import React, { useState } from 'react';
import { motion } from 'framer-motion';
import { HelpCircle, Send, Loader2 } from 'lucide-react';

const ClarificationModal = ({ taskId, missingParamsMessage, onResolved }) => {
  const [isLoading, setIsLoading] = useState(false);
  const [reply, setReply] = useState('');

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!reply.trim()) return;

    setIsLoading(true);
    
    try {
      const response = await fetch(`/task/reply?id=${taskId}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: reply })
      });

      if (!response.ok) {
        throw new Error('Failed to submit clarification reply');
      }
      
      onResolved();
    } catch (err) {
      console.error(err);
      setIsLoading(false);
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
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '1.5rem', color: 'var(--primary)' }}>
          <HelpCircle size={32} />
          <h2 style={{ color: 'var(--text-primary)', margin: 0 }}>Clarification Needed</h2>
        </div>
        
        <p style={{ color: 'var(--text-secondary)', marginBottom: '1rem' }}>
          The agent requires more information to proceed with this task.
        </p>

        <div style={{ 
          background: 'color-mix(in srgb, var(--primary) 10%, transparent)', 
          borderLeft: '4px solid var(--primary)', 
          padding: '1rem', 
          borderRadius: '0 var(--radius-sm) var(--radius-sm) 0',
          marginBottom: '1.5rem',
          color: 'var(--text-primary)',
          fontSize: '0.95rem',
          lineHeight: '1.5'
        }}>
          <strong>Missing Information:</strong><br/>
          {missingParamsMessage || "Please provide the missing parameters."}
        </div>

        <form onSubmit={handleSubmit} style={{ display: 'flex', gap: '0.5rem' }}>
          <input 
            type="text" 
            className="input-base" 
            placeholder="Type your response here..." 
            value={reply}
            onChange={(e) => setReply(e.target.value)}
            disabled={isLoading}
            autoFocus
          />
          <button 
            type="submit" 
            className="btn-primary" 
            disabled={isLoading || !reply.trim()}
          >
            {isLoading ? <Loader2 size={20} className="animate-spin" /> : <Send size={20} />}
          </button>
        </form>
      </motion.div>
    </motion.div>
  );
};

export default ClarificationModal;
