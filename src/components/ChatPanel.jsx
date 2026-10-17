import React, { useState, useRef, useEffect } from 'react';
import { Send, Loader2 } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import { apiFetch } from '../utils/api';

const ChatPanel = ({ onTaskCreated }) => {
  const governedDemoPrompt = 'Run the governed consumer-credit pre-screening demo';
  const [messages, setMessages] = useState([
    { id: 1, text: "Welcome to AOS! I'm ready to orchestrate your tasks. Try saying: 'Transfer 20 to John', 'Deploy the catalog service', or type '/help' to see more examples.", sender: 'system' }
  ]);
  const [input, setInput] = useState('');
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

      if (!response.ok) throw new Error('API Error');
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
      <div style={{ marginBottom: '0.75rem', display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
        <span style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>Controlled demo:</span>
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => setInput(governedDemoPrompt)}
          disabled={isLoading}
          title="Loads a read-only demo that AOS must first submit to Prudence"
          style={{ padding: '0.4rem 0.7rem', fontSize: '0.82rem' }}
        >
          Prudence-governed credit check
        </button>
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
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem' }}>
          <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>Simulate Role:</span>
          <select
            value={localStorage.getItem('aosMockRole') || 'admin'}
            onChange={(e) => {
              localStorage.setItem('aosMockRole', e.target.value);
              // Force re-render to update the select value visually
              setHistory(prev => [...prev]);
            }}
            style={{
              background: 'var(--bg-surface)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-light)',
              borderRadius: 'var(--radius-sm)',
              padding: '0.2rem 0.5rem',
              fontSize: '0.8rem'
            }}
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
          </select>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type your request here (↑/↓ history, e.g. 'Deploy catalog service')..."
            className="input-base"
            disabled={isLoading}
          />
          <button
            type="submit"
            className="btn btn-primary"
            disabled={isLoading || !input.trim()}
          >
            {isLoading ? <Loader2 size={18} className="spin" /> : <Send size={18} />}
          </button>
        </div>
      </form>
    </>
  );
};

export default ChatPanel;
