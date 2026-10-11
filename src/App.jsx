import React, { useState, useCallback, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import ChatPanel from './components/ChatPanel';
import Timeline from './components/Timeline';
import HumanApprovalModal from './components/HumanApprovalModal';
import ClarificationModal from './components/ClarificationModal';
import Settings from './components/Settings';
import HistoryPanel from './components/HistoryPanel';
import Logo from './components/Logo';
import LoginModal from './components/LoginModal';
import { getAuth, clearAuth } from './utils/api';
import { ssoEnabled, ssoLogin, completeSso } from './utils/oidc';
import { Bot, Settings as SettingsIcon, MessageSquare, Sun, Moon, Clock, LogIn, LogOut, UserRound, ShieldCheck } from 'lucide-react';
import './index.css';

function App() {
  const [activeTaskId, setActiveTaskId] = useState(null);
  const [pendingApproval, setPendingApproval] = useState(null);
  const [view, setView] = useState('chat'); // 'chat' | 'settings' | 'history'
  const [theme, setTheme] = useState(() => localStorage.getItem('theme') || 'dark');
  const [auth, setAuth] = useState(() => getAuth());
  const [showLogin, setShowLogin] = useState(false);

  const handleLoggedIn = (newAuth) => {
    setActiveTaskId(null);
    setPendingApproval(null);
    setAuth(newAuth);
    setShowLogin(false);
  };

  const handleLogout = () => {
    clearAuth();
    setActiveTaskId(null);
    setPendingApproval(null);
    setAuth(null);
  };

  // Complete an SSO redirect (shared Chestertown IdP) on load, if one is in flight.
  useEffect(() => {
    completeSso().then((a) => { if (a) handleLoggedIn(a); });
  }, []);

  useEffect(() => {
    localStorage.setItem('theme', theme);
    if (theme === 'light') {
      document.body.classList.add('theme-light');
    } else {
      document.body.classList.remove('theme-light');
    }
  }, [theme]);

  // Handle URL query param for taskId to support direct linking from History
  useEffect(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const taskId = urlParams.get('taskId');
    if (taskId) {
      setActiveTaskId(taskId);
      setView('chat');
    }
  }, []);

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

  const handleSelectHistoryTask = (taskId) => {
    setActiveTaskId(taskId);
    setPendingApproval(null);
    setView('chat');
  };

  return (
    <div className="app-container">
      <header className="header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', textAlign: 'left' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1.5rem' }}>
          <Logo size={64} />
          <div>
            <h1 style={{ margin: 0, fontSize: '2.2rem', lineHeight: '1.2' }}>Agentic Orchestration System</h1>
            <p style={{ margin: 0, marginTop: '0.25rem' }}>AOS Web UI - Watch your workflows execute in real-time</p>
          </div>
        </div>
        
        <div style={{ display: 'flex', gap: '1rem', flexShrink: 0, alignItems: 'center' }}>
          {/* Chestertown app-switcher — one platform, three modules (AOS is the "operate" vertex). */}
          <nav style={{ display: 'flex', gap: '4px', padding: '4px', border: '1px solid var(--border-highlight)', borderRadius: '9999px' }} aria-label="Chestertown apps">
            {[
              { label: 'AOS',      vertex: 'operate', accent: '#2563eb', url: import.meta.env.VITE_AOS_URL      || 'http://localhost:5173' },
              { label: 'Prudence', vertex: 'govern',  accent: '#d97706', url: import.meta.env.VITE_PRUDENCE_URL || 'http://localhost:5200' },
              { label: 'Ernest',   vertex: 'assure',  accent: '#0d9488', url: import.meta.env.VITE_ERNEST_URL   || 'http://localhost:5174' },
            ].map((a) => {
              const here = a.vertex === 'operate';
              return (
                <a key={a.label} href={a.url} title={`${a.label} — ${a.vertex}`}
                   style={{ fontSize: '11px', fontWeight: 600, letterSpacing: '.04em', textTransform: 'uppercase',
                            textDecoration: 'none', padding: '4px 12px', borderRadius: '9999px',
                            color: here ? '#fff' : 'var(--text-secondary)', background: here ? a.accent : 'transparent' }}>
                  {a.label}
                </a>
              );
            })}
          </nav>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: 'var(--text-secondary)', fontSize: '0.9rem' }} title={auth ? `Roles: ${(auth.roles || []).join(', ')}` : 'Anonymous dev identity'}>
            <UserRound size={16} />
            <span>{auth ? auth.userId : 'demo-user'}</span>
          </div>
          {auth ? (
            <button
              className="btn btn-secondary"
              onClick={handleLogout}
              style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 1rem' }}
              title="Sign out"
            >
              <LogOut size={18} /> Logout
            </button>
          ) : (
            <>
              {ssoEnabled && (
                <button
                  className="btn btn-primary"
                  onClick={ssoLogin}
                  style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 1rem' }}
                  title="Sign in with the shared identity provider"
                >
                  <ShieldCheck size={18} /> SSO
                </button>
              )}
              <button
                className="btn btn-secondary"
                onClick={() => setShowLogin(true)}
                style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 1rem' }}
                title="Sign in"
              >
                <LogIn size={18} /> Login
              </button>
            </>
          )}
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
            className={`btn ${view === 'history' ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setView('history')}
            style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem 1rem' }}
          >
            <Clock size={18} /> History
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
        {view === 'chat' && (
          <motion.main 
            key="chat-view"
            className="main-content"
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -20 }}
            transition={{ duration: 0.3 }}
          >
            <section className="glass-panel chat-panel">
              <ChatPanel key={auth?.userId || "demo-user"} onTaskCreated={handleTaskCreated} />
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
        )}
        
        {view === 'history' && (
          <motion.main 
            key="history-view"
            className="main-content" 
            style={{ gridTemplateColumns: '1fr', maxWidth: '1000px', margin: '0 auto' }}
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -20 }}
            transition={{ duration: 0.3 }}
          >
            <HistoryPanel key={auth?.userId || "demo-user"} onSelectTask={handleSelectHistoryTask} />
          </motion.main>
        )}

        {view === 'settings' && (
          <motion.main 
            key="settings-view"
            className="main-content" 
            style={{ gridTemplateColumns: '1fr', maxWidth: '1000px', margin: '0 auto' }}
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -20 }}
            transition={{ duration: 0.3 }}
          >
            <Settings key={auth?.userId || "demo-user"} />
          </motion.main>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {showLogin && (
          <LoginModal
            onLoggedIn={handleLoggedIn}
            onCancel={() => setShowLogin(false)}
          />
        )}

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
