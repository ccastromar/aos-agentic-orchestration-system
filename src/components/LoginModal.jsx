import React, { useState } from 'react';
import { motion } from 'framer-motion';
import { LogIn, Loader2, UserRound } from 'lucide-react';
import { login } from '../utils/api';

const ROLES = ['admin', 'user', 'finance', 'sales', 'support', 'hr', 'operations', 'sre', 'guest'];

const LoginModal = ({ onLoggedIn, onCancel }) => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState('user');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState(null);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!username.trim()) return;

    setIsLoading(true);
    setError(null);
    try {
      const auth = await login(username.trim(), password, [role]);
      onLoggedIn(auth);
    } catch (err) {
      console.error(err);
      setError('Login failed. Is the AOS server running?');
      setIsLoading(false);
    }
  };

  return (
    <motion.div
      className="modal-overlay"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      onClick={onCancel}
    >
      <motion.div
        initial={{ opacity: 0, scale: 0.9, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.9, y: 20 }}
        transition={{ type: "spring", bounce: 0.4 }}
        className="glass-panel modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '1rem', color: 'var(--primary)' }}>
          <UserRound size={32} />
          <h2 style={{ color: 'var(--text-primary)', margin: 0 }}>Sign in</h2>
        </div>

        <p style={{ color: 'var(--text-secondary)', marginBottom: '1.5rem', fontSize: '0.9rem' }}>
          Dev login: credentials are not validated yet. Your username scopes your
          operation history; the role drives RBAC.
        </p>

        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
          <input
            type="text"
            className="input-base"
            placeholder="Username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            disabled={isLoading}
            autoFocus
          />
          <input
            type="password"
            className="input-base"
            placeholder="Password (ignored for now)"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={isLoading}
          />
          <select
            className="input-base"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            disabled={isLoading}
          >
            {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
          </select>

          {error && (
            <div style={{ color: 'var(--danger, #e5484d)', fontSize: '0.9rem' }}>{error}</div>
          )}

          <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', marginTop: '0.5rem' }}>
            <button type="button" className="btn btn-secondary" onClick={onCancel} disabled={isLoading}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={isLoading || !username.trim()} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              {isLoading ? <Loader2 size={18} className="animate-spin" /> : <LogIn size={18} />} Sign in
            </button>
          </div>
        </form>
      </motion.div>
    </motion.div>
  );
};

export default LoginModal;
