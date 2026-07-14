import React, { useState, useCallback } from 'react';
import ChatPanel from './components/ChatPanel';
import Timeline from './components/Timeline';
import HumanApprovalModal from './components/HumanApprovalModal';
import ClarificationModal from './components/ClarificationModal';
import { Bot } from 'lucide-react';
import './index.css';

function App() {
  const [activeTaskId, setActiveTaskId] = useState(null);
  const [pendingApproval, setPendingApproval] = useState(null);

  const handleTaskCreated = (taskId) => {
    setActiveTaskId(taskId);
    setPendingApproval(null);
  };

  const handleApprovalNeeded = useCallback((id, gate, message) => {
    setPendingApproval(prev => {
      if (!prev) {
        return { id, gate, message };
      }
      return prev;
    });
  }, []);

  const handleApprovalResolved = () => {
    setPendingApproval(null);
  };

  return (
    <div className="app-container">
      <header className="header">
        <Bot size={48} className="mx-auto text-blue-500 mb-2" style={{ color: 'var(--primary)' }} />
        <h1>Agent Orchestration System</h1>
        <p>AOS Web UI - Watch your agents think and act in real-time</p>
      </header>

      <main className="main-content">
        <section className="glass-panel chat-panel">
          <ChatPanel onTaskCreated={handleTaskCreated} />
        </section>

        <section className="glass-panel timeline-container">
          {activeTaskId ? (
            <Timeline 
              taskId={activeTaskId} 
              onApprovalNeeded={handleApprovalNeeded} 
            />
          ) : (
            <div style={{ textAlign: 'center', marginTop: '4rem', color: 'var(--text-muted)' }}>
              <Bot size={64} opacity={0.2} style={{ margin: '0 auto 1rem' }} />
              <h2>No active task</h2>
              <p>Send a message to wake up the agents.</p>
            </div>
          )}
        </section>
      </main>

      {pendingApproval && pendingApproval.gate === 'clarification' && (
        <ClarificationModal 
          taskId={pendingApproval.id} 
          missingParamsMessage={pendingApproval.message}
          onResolved={handleApprovalResolved}
        />
      )}
      
      {pendingApproval && pendingApproval.gate !== 'clarification' && (
        <HumanApprovalModal 
          taskId={pendingApproval.id} 
          gate={pendingApproval.gate}
          message={pendingApproval.message}
          onResolved={handleApprovalResolved}
        />
      )}
    </div>
  );
}

export default App;
