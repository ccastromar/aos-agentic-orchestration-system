import React, { useState, useCallback, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import ChatPanel from './components/ChatPanel';
import Timeline from './components/Timeline';
import HumanApprovalModal from './components/HumanApprovalModal';
import ClarificationModal from './components/ClarificationModal';
import Settings from './components/Settings';
import { Bot, Settings as SettingsIcon, MessageSquare, Sun, Moon } from 'lucide-react';
import './index.css';

function App() {
  const [activeTaskId, setActiveTaskId] = useState(null);
  const [pendingApproval, setPendingApproval] = useState(null);
  const [view, setView] = useState('chat'); // 'chat' | 'settings'
  const [theme, setTheme] = useState(() => localStorage.getItem('theme') || 'dark');

  useEffect(() => {
    localStorage.setItem('theme', theme);
    if (theme === 'light') {
      document.body.classList.add('theme-light');
    } else {
      document.body.classList.remove('theme-light');
    }
  }, [theme]);

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
      <header className="header" style={{ position: 'relative' }}>
        <Bot size={48} className="mx-auto text-blue-500 mb-2" style={{ color: 'var(--primary)' }} />
        <h1>Augmented Orchestration System</h1>
        <p>AOS Web UI - Watch your workflows execute in real-time</p>
        
        <div style={{ position: 'absolute', top: '1rem', right: '2rem', display: 'flex', gap: '1rem' }}>
          <button 
            className="btn btn-secondary"
            onClick={() => setTheme(t => t === 'light' ? 'dark' : 'light')}
            style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '0.5rem', borderRadius: '50%' }}
            title={`Switch to ${theme === 'light' ? 'dark' : 'light'} mode`}
          >
            {theme === 'light' ? <Moon size={18} /> : <Sun size={18} />}
          </button>
          
          <button 
            className={`btn ${view === 'chat' ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setView('chat')}
            style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 1rem' }}
          >
            <MessageSquare size={18} /> Chat
          </button>
          <button 
            className={`btn ${view === 'settings' ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setView('settings')}
            style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 1rem' }}
          >
            <SettingsIcon size={18} /> Settings
          </button>
        </div>
      </header>

      <AnimatePresence mode="wait">
        {view === 'chat' ? (
          <motion.main 
            key="chat-view"
            className="main-content"
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -20 }}
            transition={{ duration: 0.3 }}
          >
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
                <motion.div 
                  initial={{ opacity: 0, scale: 0.95 }}
                  animate={{ opacity: 1, scale: 1 }}
                  style={{ textAlign: 'center', marginTop: '4rem', color: 'var(--text-muted)' }}
                >
                  <Bot size={64} opacity={0.2} style={{ margin: '0 auto 1rem' }} />
                  <h2>No active task</h2>
                  <p>Send a message to wake up the agents.</p>
                </motion.div>
              )}
            </section>
          </motion.main>
        ) : (
          <motion.main 
            key="settings-view"
            className="main-content" 
            style={{ gridTemplateColumns: '1fr', maxWidth: '1000px', margin: '0 auto' }}
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -20 }}
            transition={{ duration: 0.3 }}
          >
            <Settings />
          </motion.main>
        )}
      </AnimatePresence>

      <AnimatePresence>
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
      </AnimatePresence>
    </div>
  );
}

export default App;
