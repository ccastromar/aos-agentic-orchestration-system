import React, { useState, useRef, useEffect } from 'react';
import { Send, Loader2 } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import { apiFetch, getAuth, savePendingPrompt, takePendingPrompt } from '../utils/api';
import { devLoginEnabled, ssoEnabled } from '../utils/oidc';
import { Button, Input, Select } from '@chestertown/design';

const ChatPanel = ({ onTaskCreated }) => {
  // One prompt per Prudence risk-tier case of the live demo. The deterministic mock models
  // pin these phrases (mocks/ollama/scenarios.yaml); the support phrase must stay verbatim
  // because the deployed aos-mock-agent recognises only that exact text.
  const governedDemos = [
    { label: 'Spam check · minimal risk', prompt: 'Check message MSG-2207 for spam', title: 'Minimal-risk system: already approved in Prudence, so it runs' },
    { label: 'Support lookup · limited risk', prompt: 'Run the governed customer-support lookup demo', title: 'Limited-risk system: denied until one reviewer approves it in Prudence' },
    { label: 'Credit pre-screening · high risk', prompt: 'Pre-screen credit application APP-1042', title: 'High-risk system: blocked until two independent reviewers approve it in Prudence' },
    { label: 'Social scoring · prohibited', prompt: 'Compute the social score of citizen CIT-7781', title: 'Prohibited practice: Prudence always denies it' },
    { label: 'Translator · suspended', prompt: 'Translate "the branch opens at nine" into Portuguese', title: 'Suspended system: denied until it is resubmitted and approved in Prudence' },
  ];
  const [messages, setMessages] = useState([
    { id: 1, text: "Welcome to AOS. I run predefined, governed pipelines; I don't create workflows on demand. Use one of the available flows—for example, 'Transfer 20 to John' or 'Deploy the catalog service'—or type '/help' to see supported examples.", sender: 'system' }
  ]);
  const [input, setInput] = useState(() => (getAuth()?.token ? takePendingPrompt() : ''));
  const [isLoading, setIsLoading] = useState(false);

  // Keep the last ten prompts across reloads so ↑/↓ is useful between sessions.
  const [history, setHistory] = useState(() => {
    try {
      const saved = JSON.parse(localStorage.getItem('aosChatHistory') || '[]');
      return Array.isArray(saved) ? saved.filter(Boolean).slice(-10) : [];
    } catch {
      return [];
    }
  });
  const [historyIndex, setHistoryIndex] = useState(-1);
  // Mock roles are useful only when exercising anonymous development flows. A real
  // SSO or development-login session supplies the identity and roles in its token.
  const showMockRole = devLoginEnabled && !getAuth()?.token;

  const messagesEndRef = useRef(null);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages]);

  useEffect(() => {
    localStorage.setItem('aosChatHistory', JSON.stringify(history.slice(-10)));
  }, [history]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!input.trim() || isLoading) return;

    const userMsg = input.trim();

    // Update history
    setHistory(prev => [...prev.filter(msg => msg !== userMsg), userMsg].slice(-10));
    setHistoryIndex(-1);

    setInput('');
    setMessages(prev => [...prev, { id: Date.now(), text: userMsg, sender: 'user' }]);

    if (userMsg.toLowerCase() === '/help') {
      try {
        const res = await apiFetch('/intents');
        const data = await res.json();
        if (data && data.length > 0) {
          const examplesText = data.map(i => {
            if (i.example) return `• "${i.example}"`;
            return `• ${i.description.trim()}`;
          }).join('\n');
          setMessages(prev => [...prev, {
            id: Date.now() + 1,
            text: `Here are some things you can ask me:\n${examplesText}`,
            sender: 'system'
          }]);
        }
      } catch (err) {
        console.error("Error fetching intents:", err);
      }
      return;
    }

    setIsLoading(true);

    try {
      const response = await apiFetch('/ask', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: userMsg, lang: 'en' })
      });

      if (response.status === 401) {
        savePendingPrompt(userMsg);
        throw new Error(ssoEnabled || devLoginEnabled
          ? 'Your session has expired or you are not signed in. Please sign in to continue.'
          : 'You are not signed in.');
      }
      if (!response.ok) throw new Error(`Request failed (${response.status})`);
      const data = await response.json();

      onTaskCreated(data.id);

      setMessages(prev => [...prev, {
        id: Date.now(),
        text: `Task dispatched! Watch the timeline to see agents thinking.`,
        taskId: data.id,
        sender: 'system'
      }]);
    } catch (err) {
      setMessages(prev => [...prev, { id: Date.now(), text: `Error: ${err.message}`, sender: 'system', isError: true }]);
    } finally {
      setIsLoading(false);
    }
  };

  const handleKeyDown = (e) => {
    if (history.length === 0 || isLoading) return;

    if (e.key === 'ArrowUp') {
      e.preventDefault();
      const newIndex = historyIndex === -1 ? history.length - 1 : Math.max(0, historyIndex - 1);
      setHistoryIndex(newIndex);
      setInput(history[newIndex]);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (historyIndex !== -1) {
        const newIndex = historyIndex + 1;
        if (newIndex >= history.length) {
          setHistoryIndex(-1);
          setInput('');
        } else {
          setHistoryIndex(newIndex);
          setInput(history[newIndex]);
        }
      }
    }
  };

  return (
    <>
      <div style={{ marginBottom: '1rem', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '0.5rem' }}>
        <span style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>Prudence-governed demos:</span>
        <div style={{ display: 'flex', justifyContent: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
          {governedDemos.map(demo => (
            <Button
              key={demo.label}
              size="sm"
              onClick={() => setInput(demo.prompt)}
              disabled={isLoading}
              title={demo.title}
            >
              {demo.label}
            </Button>
          ))}
        </div>
      </div>
      <div className="chat-messages">
        <AnimatePresence initial={false}>
          {messages.map((msg) => (
              <motion.div
                key={msg.id}
                initial={{ opacity: 0, scale: 0.8, y: 20, originX: msg.sender === 'user' ? 1 : 0 }}
                animate={{ opacity: 1, scale: 1, y: 0 }}
                transition={{ type: "spring", stiffness: 400, damping: 25 }}
                layout
                style={{
                  alignSelf: msg.sender === 'user' ? 'flex-end' : 'flex-start',
                  background: msg.sender === 'user' ? 'linear-gradient(135deg, var(--primary), color-mix(in srgb, var(--primary) 70%, white))' : msg.isError ? 'color-mix(in srgb, #ef4444 15%, transparent)' : 'color-mix(in srgb, var(--bg-surface) 60%, transparent)',
                  border: msg.sender === 'user' ? 'none' : `1px solid ${msg.isError ? 'color-mix(in srgb, #ef4444 40%, transparent)' : 'var(--border-light)'}`,
                  padding: '0.85rem 1.25rem',
                  borderRadius: msg.sender === 'user' ? '20px 20px 4px 20px' : '20px 20px 20px 4px',
                  maxWidth: '85%',
                  fontSize: '0.95rem',
                  boxShadow: msg.sender === 'user' ? '0 4px 15px var(--primary-glow)' : '0 4px 15px rgba(0,0,0,0.1)',
                  whiteSpace: 'pre-wrap',
                  backdropFilter: 'blur(12px)',
                  color: msg.isError ? '#FCA5A5' : 'var(--text-primary)'
                }}>
                {msg.text}
                {msg.taskId && (
                  <div
                    onClick={() => onTaskCreated(msg.taskId)}
                    style={{
                      marginTop: '0.75rem',
                      padding: '0.5rem 0.75rem',
                      background: 'rgba(255, 255, 255, 0.05)',
                      borderRadius: 'var(--radius-sm)',
                      cursor: 'pointer',
                      fontSize: '0.85rem',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '0.5rem',
                      border: '1px solid var(--border-light)',
                      transition: 'all var(--transition-fast)'
                    }}
                    onMouseOver={(e) => {
                      e.currentTarget.style.background = 'color-mix(in srgb, var(--primary) 10%, transparent)';
                      e.currentTarget.style.borderColor = 'var(--primary)';
                    }}
                    onMouseOut={(e) => {
                      e.currentTarget.style.background = 'rgba(255, 255, 255, 0.05)';
                      e.currentTarget.style.borderColor = 'var(--border-light)';
                    }}
                  >
                    <span style={{ fontWeight: '600', color: 'var(--primary)' }}>Task ID:</span>
                    <span style={{ fontFamily: 'monospace' }}>{msg.taskId.substring(0,8)}</span>
                    <span style={{ marginLeft: 'auto', fontSize: '0.75rem', opacity: 0.8 }}>View Timeline ➔</span>
                  </div>
                )}
              </motion.div>
          ))}
        </AnimatePresence>
        <div ref={messagesEndRef} />
      </div>

      <form onSubmit={handleSubmit} className="chat-input-form">
        {showMockRole && (
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem' }}>
            <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>Demo role:</span>
            <Select
              value={localStorage.getItem('aosMockRole') || 'admin'}
              onChange={(e) => {
                localStorage.setItem('aosMockRole', e.target.value);
                // Force re-render to update the select value visually.
                setHistory(prev => [...prev]);
              }}
              aria-label="Demo role"
              className="ct-text-sm"
              style={{ width: 'auto', padding: '4px 8px' }}
            >
              <option value="admin">Admin</option>
              <option value="user">User</option>
              <option value="guest">Guest</option>
              <option value="finance">Finance</option>
              <option value="sales">Sales</option>
              <option value="support">Support</option>
              <option value="sre">SRE / DevOps</option>
              <option value="hr">Human Resources</option>
              <option value="operations">Operations</option>
            </Select>
          </div>
        )}
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type your request here (↑/↓ history, e.g. 'Deploy catalog service')..."
            aria-label="Your request"
            disabled={isLoading}
          />
          <Button type="submit" variant="primary" disabled={isLoading || !input.trim()} aria-label="Send">
            {isLoading ? <Loader2 size={18} className="spin" /> : <Send size={18} />}
          </Button>
        </div>
      </form>
    </>
  );
};

export default ChatPanel;
