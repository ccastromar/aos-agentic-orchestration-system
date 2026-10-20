import React, { useState, useCallback, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import ChatPanel from './components/ChatPanel';
import Timeline from './components/Timeline';
import HumanApprovalModal from './components/HumanApprovalModal';
import ClarificationModal from './components/ClarificationModal';
import Settings from './components/Settings';
import HistoryPanel from './components/HistoryPanel';
import LoginModal from './components/LoginModal';
import { getAuth, clearAuth } from './utils/api';
import { ssoEnabled, devLoginEnabled, ssoLogin, completeSso } from './utils/oidc';
import { Bot, Settings as SettingsIcon, MessageSquare, Sun, Moon, Clock, LogIn, LogOut, UserRound, ShieldCheck } from 'lucide-react';
import './index.css';

function App() {
  const [activeTaskId, setActiveTaskId] = useState(null);
  const [pendingApproval, setPendingApproval] = useState(null);
  const [view, setView] = useState('chat'); // 'chat' | 'settings' | 'history'
  const [theme, setTheme] = useState(() => localStorage.getItem('theme') || 'light');
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
    document.documentElement.dataset.theme = theme;
    document.documentElement.dataset.vertex = 'operate';
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
      <header className="topbar">
        <div className="brand"><span className="mark">A</span><span>AOS</span><span className="vertex">· operate</span></div>
        <div className="topbar-actions">
          <nav className="switcher" aria-label="Chestertown apps">
            {[
              { label: 'AOS',      vertex: 'operate', url: import.meta.env.VITE_AOS_URL      || 'http://localhost:5173' },
              { label: 'Prudence', vertex: 'govern',  url: import.meta.env.VITE_PRUDENCE_URL || 'http://localhost:5200' },
              { label: 'Ernest',   vertex: 'assure',  url: import.meta.env.VITE_ERNEST_URL   || 'http://localhost:5174' },
            ].map((a) => {
              const here = a.vertex === 'operate';
              return (
                <a key={a.label} href={a.url} title={`${a.label} — ${a.vertex}`}
                   className={here ? 'here' : ''}>
                  {a.label}
                </a>
              );
            })}
          </nav>
          <div className="identity" title={auth ? `Roles: ${(auth.roles || []).join(', ')}` : 'Anonymous'}>
            <UserRound size={16} />
            <span>{auth ? auth.userId : (devLoginEnabled ? 'demo-user' : 'Not signed in')}</span>
          </div>
          {auth ? (
            <button
              className="btn btn-secondary"
              onClick={handleLogout}
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
                  title="Sign in with the shared identity provider"
                >
                  <ShieldCheck size={18} /> SSO
                </button>
              )}
              {devLoginEnabled && (
                <button
                  className="btn btn-secondary"
                  onClick={() => setShowLogin(true)}
                  title="Development login"
                >
                  <LogIn size={18} /> Dev login
                </button>
              )}
            </>
          )}
          <button
            className="btn btn-secondary"
            onClick={() => setTheme(t => t === 'light' ? 'dark' : 'light')}
            title={`Switch to ${theme === 'light' ? 'dark' : 'light'} mode`}
          >
            {theme === 'light' ? <Moon size={18} /> : <Sun size={18} />}
          </button>
          
        </div>
      </header>
      <nav className="tabs" aria-label="AOS sections">
        <button className={view === 'chat' ? 'active' : ''} onClick={() => setView('chat')}><MessageSquare size={16} /> Chat</button>
        <button className={view === 'history' ? 'active' : ''} onClick={() => setView('history')}><Clock size={16} /> History</button>
        <button className={view === 'settings' ? 'active' : ''} onClick={() => setView('settings')}><SettingsIcon size={16} /> Settings</button>
      </nav>

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
