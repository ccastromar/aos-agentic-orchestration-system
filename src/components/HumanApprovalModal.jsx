import React, { useState } from 'react';
import { AlertTriangle, Check, X, Loader2 } from 'lucide-react';
import { Alert, Button, Modal } from '@chestertown/design';
import { apiFetch } from '../utils/api';

const HumanApprovalModal = ({ taskId, gate, message, onResolved }) => {
  const [isLoading, setIsLoading] = useState(false);
  const [action, setAction] = useState(null);
  const [error, setError] = useState(null);

  const handleDecision = async (approved) => {
    setIsLoading(true);
    setAction(approved ? 'approve' : 'reject');
    setError(null);

    try {
      const endpoint = approved ? 'task/approve' : 'task/reject';
      // URL: POST /task/approve?id=...&gate=...
      const response = await apiFetch(`/${endpoint}?id=${taskId}&gate=${gate}`, {
        method: 'POST'
      });

      if (!response.ok) {
        throw new Error('Failed to submit decision');
      }

      onResolved();
    } catch (err) {
      console.error(err);
      setError('The decision could not be sent. Try again.');
      setIsLoading(false);
      setAction(null);
    }
  };

  // No onClose: the pipeline is paused until a person approves or rejects.
  return (
    <Modal
      title="Human approval required"
      icon={<AlertTriangle size={28} />}
      actions={
        <>
          <Button onClick={() => handleDecision(false)} disabled={isLoading}>
            {isLoading && action === 'reject' ? <Loader2 size={16} className="animate-spin" /> : <X size={16} />}
            Reject
          </Button>
          <Button variant="primary" onClick={() => handleDecision(true)} disabled={isLoading}>
            {isLoading && action === 'approve' ? <Loader2 size={16} className="animate-spin" /> : <Check size={16} />}
            Approve action
          </Button>
        </>
      }
    >
      <p className="ct-muted ct-mt-0">
        The agent pipeline has paused execution because a tool requires explicit human authorization.
      </p>
      {message && (
        <Alert tone="warn" title="Action context" className="ct-mb-4">
          <div style={{ whiteSpace: 'pre-wrap', maxHeight: 250, overflowY: 'auto' }}>{message}</div>
        </Alert>
      )}
      <dl className="ct-text-sm ct-mb-0">
        <dt className="ct-muted">Task ID</dt>
        <dd style={{ margin: '0 0 var(--sp-2)' }}><code>{taskId}</code></dd>
        <dt className="ct-muted">Gate</dt>
        <dd style={{ margin: 0 }}><code>{gate}</code></dd>
      </dl>
      {error && <Alert tone="danger" className="ct-mt-4">{error}</Alert>}
    </Modal>
  );
};

export default HumanApprovalModal;
