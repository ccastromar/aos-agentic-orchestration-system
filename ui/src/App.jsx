import React, { useState, useCallback, useEffect, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import ChatPanel from './components/ChatPanel';
import Timeline from './components/Timeline';
import HumanApprovalModal from './components/HumanApprovalModal';
import ClarificationModal from './components/ClarificationModal';
import Settings from './components/Settings';
import HistoryPanel from './components/HistoryPanel';
import LoginModal from './components/LoginModal';
import { apiFetch, getAuth, clearAuth, UNAUTHORIZED_EVENT, NO_ROLES_EVENT } from './utils/api';
import { ssoEnabled, devLoginEnabled, ssoLogin, completeSso, markSignedOut, watchAosSession, trySilentSso } from './utils/oidc';
import { Bot, Settings as SettingsIcon, MessageSquare, Clock, LogIn, LogOut, ShieldCheck } from 'lucide-react';
import { Button, Card, Identity, Tabs, ThemeToggle, Topbar, chestertownApps } from '@chestertown/design';
import '@chestertown/design/styles.css';
import './index.css';

const APPS = chestertownApps({
  operate: import.meta.env.VITE_AOS_URL,
  govern: import.meta.env.VITE_PRUDENCE_URL,
  assure: import.meta.env.VITE_ERNEST_URL,
});

const PENDING_TASK_KEY = 'aosPendingTaskId';

const TAB_ITEMS = [
  { id: 'chat', label: 'Chat', icon: <MessageSquare size={16} /> },
  { id: 'history', label: 'History', icon: <Clock size={16} /> },
  { id: 'settings', label: 'Settings', icon: <SettingsIcon size={16} /> },
];

function App() {
  const [activeTaskId, setActiveTaskId] = useState(null);
  const [pendingApproval, setPendingApproval] = useState(null);
  const [view, setView] = useState('chat'); // 'chat' | 'settings' | 'history'
  const [auth, setAuth] = useState(() => getAuth());
  const [showLogin, setShowLogin] = useState(false);
  const [noRoles, setNoRoles] = useState(false);
  const [ready, setReady] = useState(!ssoEnabled);

  useEffect(() => {
    const onNoRoles = () => setNoRoles(true);
    window.addEventListener(NO_ROLES_EVENT, onNoRoles);
    return () => window.removeEventListener(NO_ROLES_EVENT, onNoRoles);
  }, []);

  // A signed-in person whose token grants no function in AOS is told so at once, not at their
  // first request: any API call answers 403 and apiFetch raises NO_ROLES_EVENT.
  useEffect(() => {
    if (!ssoEnabled || !auth?.token) return;
    apiFetch('/api/v1/tasks').catch(() => {});
  }, [auth]);

  const handleLoggedIn = (newAuth) => {
    setNoRoles(false);
    setActiveTaskId(null);
    setPendingApproval(null);
    setAuth(newAuth);
    setShowLogin(false);
  };

  const handleLogout = () => {
    clearAuth();
    markSignedOut();
    setNoRoles(false);
    setActiveTaskId(null);
    setPendingApproval(null);
    setAuth(null);
  };

  // A 401 from any API call means no session or an expired one: go to the sign-in flow
  // instead of surfacing a raw API error. The ref keeps parallel 401s from starting two
  // SSO redirects (the second would overwrite the PKCE state of the first).
  const redirectingRef = useRef(false);
  useEffect(() => {
    const onUnauthorized = () => {
      setAuth(null);
      if (ssoEnabled) {
        if (redirectingRef.current) return;
        redirectingRef.current = true;
        // The IdP returns to "/", so keep a deep-linked run (?taskId=, e.g. from Ernest) to reopen it.
        const taskId = new URLSearchParams(window.location.search).get('taskId');
        if (taskId) sessionStorage.setItem(PENDING_TASK_KEY, taskId);
        ssoLogin();
      } else if (devLoginEnabled) {
        setShowLogin(true);
      }
    };
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, []);

  // Another app of the suite signed out: the IdP session is gone, so drop the token we still hold.
  useEffect(() => {
    if (!ssoEnabled) return;
    return watchAosSession(() => Boolean(getAuth()), () => {
      clearAuth();
      setNoRoles(false);
      setActiveTaskId(null);
      setPendingApproval(null);
      setAuth(null);
    });
  }, []);

  // Complete an SSO redirect (shared Chestertown IdP) on load, if one is in flight.
  useEffect(() => {
    completeSso().then(async (a) => {
      if (!a && !getAuth()) {
        const taskId = new URLSearchParams(window.location.search).get('taskId');
        if (taskId) sessionStorage.setItem(PENDING_TASK_KEY, taskId);
        if (await trySilentSso()) return;
      }
      setReady(true);
      if (!a) return;
      handleLoggedIn(a);
      const taskId = sessionStorage.getItem(PENDING_TASK_KEY);
      sessionStorage.removeItem(PENDING_TASK_KEY);
      if (taskId) {
        setActiveTaskId(taskId);
        setView('chat');
      }
    });
  }, []);

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

  const topbar = (
  <Topbar
    vertex="operate"
    name="AOS"
    apps={APPS}
    actions={
      <>
        <Identity
          name={auth ? auth.userId : (devLoginEnabled ? 'demo-user' : null)}
          detail={auth ? `Roles: ${(auth.roles || []).join(', ')}` : 'Anonymous'}
        />
        {auth ? (
          <Button size="sm" onClick={handleLogout} title="Sign out">
            <LogOut size={14} /> Sign out
          </Button>
        ) : (
          <>
            {ssoEnabled && (
              <Button size="sm" variant="primary" onClick={ssoLogin} title="Sign in with the shared identity provider">
                <ShieldCheck size={14} /> Sign in
              </Button>
            )}
            {devLoginEnabled && (
              <Button size="sm" onClick={() => setShowLogin(true)} title="Development login">
                <LogIn size={14} /> Dev login
              </Button>
            )}
          </>
        )}
        <ThemeToggle storageKey="theme" />
      </>
    }
  />
  );

  const signInFromGate = () => {
    const taskId = new URLSearchParams(window.location.search).get('taskId');
    if (taskId) sessionStorage.setItem(PENDING_TASK_KEY, taskId);
    ssoLogin();
  };

  if (!ready) return null;

  if (ssoEnabled && !auth) {
    return (
      <div className="app-container">
        {topbar}
        <Card style={{ maxWidth: 420, margin: 'var(--sp-12) auto', textAlign: 'center' }}>
          <h1 className="ct-title">Sign in to AOS</h1>
          <p className="sub">Running intents is a controlled function — sign in with your identity provider.</p>
          <Button variant="primary" onClick={signInFromGate}>
            <ShieldCheck size={14} /> Sign in with SSO
          </Button>
        </Card>
      </div>
    );
  }

  if (noRoles) {
    return (
      <div className="app-container">
        {topbar}
        <Card style={{ maxWidth: 480, margin: 'var(--sp-12) auto', textAlign: 'center' }}>
          <h1 className="ct-title">No access to AOS</h1>
          <p className="sub">
            {auth?.userId || 'Your account'} is signed in but has no AOS role. Ask an administrator
            to assign one, then sign in again.
          </p>
        </Card>
      </div>
    );
  }

  return (
    <div className="app-container">
      {topbar}
      <Tabs label="AOS sections" items={TAB_ITEMS} value={view} onChange={setView} />

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
