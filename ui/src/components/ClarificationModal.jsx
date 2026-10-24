import React, { useState } from 'react';
import { HelpCircle, Send, Loader2 } from 'lucide-react';
import { Alert, Button, Field, Input, Modal } from '@chestertown/design';
import { apiFetch } from '../utils/api';

const ClarificationModal = ({ taskId, missingParamsMessage, onResolved }) => {
  const [isLoading, setIsLoading] = useState(false);
  const [reply, setReply] = useState('');
  const [error, setError] = useState(null);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!reply.trim()) return;

    setIsLoading(true);
    setError(null);

    try {
      const response = await apiFetch(`/task/reply?id=${taskId}`, {
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
      setError('The reply could not be sent. Try again.');
      setIsLoading(false);
    }
  };

  // No onClose: the task waits for this answer.
  return (
    <Modal title="Clarification needed" icon={<HelpCircle size={28} />}>
      <p className="ct-muted ct-mt-0">The agent requires more information to proceed with this task.</p>
      <Alert tone="info" title="Missing information" className="ct-mb-4">
        {missingParamsMessage || 'Please provide the missing parameters.'}
      </Alert>
      <form onSubmit={handleSubmit}>
        <Field label="Your answer" htmlFor="clarification-reply">
          <div className="ct-row" style={{ flexWrap: 'nowrap' }}>
            <Input id="clarification-reply" placeholder="Type your response here…" value={reply}
                   onChange={(e) => setReply(e.target.value)} disabled={isLoading} autoFocus />
            <Button type="submit" variant="primary" disabled={isLoading || !reply.trim()} aria-label="Send answer">
              {isLoading ? <Loader2 size={16} className="animate-spin" /> : <Send size={16} />}
            </Button>
          </div>
        </Field>
      </form>
      {error && <Alert tone="danger">{error}</Alert>}
    </Modal>
  );
};

export default ClarificationModal;
