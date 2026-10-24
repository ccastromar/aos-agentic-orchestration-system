import React, { useState } from 'react';
import { LogIn, Loader2, UserRound } from 'lucide-react';
import { Alert, Button, Field, Input, Modal, Select } from '@chestertown/design';
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
    <Modal
      title="Sign in"
      icon={<UserRound size={28} />}
      onClose={onCancel}
      actions={
        <>
          <Button onClick={onCancel} disabled={isLoading}>Cancel</Button>
          <Button type="submit" form="aos-dev-login" variant="primary" disabled={isLoading || !username.trim()}>
            {isLoading ? <Loader2 size={16} className="animate-spin" /> : <LogIn size={16} />} Sign in
          </Button>
        </>
      }
    >
      <p className="ct-muted ct-text-sm ct-mt-0">
        Dev login: credentials are not validated yet. Your username scopes your
        operation history; the role drives RBAC.
      </p>
      <form id="aos-dev-login" onSubmit={handleSubmit}>
        <Field label="Username" htmlFor="login-username">
          <Input id="login-username" value={username} onChange={(e) => setUsername(e.target.value)}
                 disabled={isLoading} autoFocus autoComplete="username" />
        </Field>
        <Field label="Password" htmlFor="login-password" hint="Ignored for now.">
          <Input id="login-password" type="password" value={password} onChange={(e) => setPassword(e.target.value)}
                 disabled={isLoading} autoComplete="current-password" />
        </Field>
        <Field label="Role" htmlFor="login-role">
          <Select id="login-role" value={role} onChange={(e) => setRole(e.target.value)} disabled={isLoading}>
            {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
          </Select>
        </Field>
      </form>
      {error && <Alert tone="danger">{error}</Alert>}
    </Modal>
  );
};

export default LoginModal;
