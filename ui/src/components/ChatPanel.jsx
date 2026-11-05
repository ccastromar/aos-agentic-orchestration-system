import React, { useState, useRef, useEffect } from 'react';
import { Send, Loader2, ArrowRight } from 'lucide-react';
import { format } from 'date-fns';
import { apiFetch, getAuth, savePendingPrompt, takePendingPrompt } from '../utils/api';
import { devLoginEnabled, ssoEnabled } from '../utils/oidc';
import { taskStatus } from '../utils/taskStatus';
import { Badge, Button, Input, Select } from '@chestertown/design';

// The chat is a thread of requests and what AOS did with each, not a conversation: AOS runs
// predefined pipelines, and each dispatched task shows as a card with its current status.
const ChatPanel = ({ onTaskCreated, activeTaskId, taskStatuses = {} }) => {
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
    setMessages(prev => [...prev, { id: Date.now(), text: userMsg, sender: 'user', at: new Date() }]);

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
            sender: 'system',
            at: new Date()
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
        text: 'Task dispatched.',
        taskId: data.id,
        sender: 'system',
        at: new Date()
      }]);
    } catch (err) {
      setMessages(prev => [...prev, { id: Date.now(), text: err.message, sender: 'system', isError: true, at: new Date() }]);
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
      <ol className="chat-messages aos-thread" aria-label="Requests and replies" aria-live="polite">
        {messages.map((msg) => {
          const status = msg.taskId ? taskStatus(taskStatuses[msg.taskId] || 'pending') : null;
          return (
            <li key={msg.id} className={`aos-msg aos-msg--${msg.isError ? 'error' : msg.sender}`}>
              <div className="aos-msg-meta">
                {msg.sender === 'user' ? 'You' : 'AOS'}
                {msg.at && <> · {format(msg.at, 'HH:mm')}</>}
              </div>
              {msg.text && <div className="aos-msg-text">{msg.text}</div>}
              {msg.taskId && (
                <button
                  type="button"
                  className={`aos-task-card${msg.taskId === activeTaskId ? ' aos-task-card--active' : ''}`}
                  onClick={() => onTaskCreated(msg.taskId)}
                  aria-label={`Open run ${msg.taskId.substring(0, 8)}: ${status.label}`}
                >
                  <span className="aos-mono">{msg.taskId.substring(0, 8)}</span>
                  <Badge tone={status.tone} dot>{status.label}</Badge>
                  <span className="aos-task-card-open">View run <ArrowRight size={14} aria-hidden="true" /></span>
                </button>
              )}
            </li>
          );
        })}
        <li ref={messagesEndRef} aria-hidden="true" />
      </ol>

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
