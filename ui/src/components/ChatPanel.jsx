import React, { useState, useRef, useEffect } from 'react';
import { Send, Loader2 } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';

const ChatPanel = ({ onTaskCreated }) => {
  const [messages, setMessages] = useState([
    { id: 1, text: "Welcome to AOS! I'm ready to orchestrate your tasks. Try saying: 'Transfer 20 to John' or 'Deploy the catalog service'", sender: 'system' }
  ]);
  const [input, setInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  
  // History state for Arrow Up/Down navigation
  const [history, setHistory] = useState([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  
  const messagesEndRef = useRef(null);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!input.trim() || isLoading) return;

    const userMsg = input.trim();
    
    // Update history
    setHistory(prev => [...prev, userMsg]);
    setHistoryIndex(-1);
    
    setInput('');
    setMessages(prev => [...prev, { id: Date.now(), text: userMsg, sender: 'user' }]);

    if (userMsg.toLowerCase() === '/help') {
      try {
        const res = await fetch('/intents');
        const data = await res.json();
        if (data && data.length > 0) {
          const examplesText = data.map(i => `• ${i.description}`).join('\n');
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
      const response = await fetch('/ask', {
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
    if (history.length === 0) return;

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

      <form onSubmit={handleSubmit} className="chat-input-area">
        <input 
          type="text" 
          className="input-base" 
          placeholder="Ask something..." 
          value={input}
          onChange={(e) => {
            setInput(e.target.value);
            // Reset history index if user types manually
            if (historyIndex !== -1) setHistoryIndex(-1);
          }}
          onKeyDown={handleKeyDown}
          disabled={isLoading}
        />
        <button type="submit" className="btn-primary" disabled={isLoading || !input.trim()}>
          {isLoading ? <Loader2 size={20} className="animate-spin" /> : <Send size={20} />}
        </button>
      </form>
    </>
  );
};

export default ChatPanel;
