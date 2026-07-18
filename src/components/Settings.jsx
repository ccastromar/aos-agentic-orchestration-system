import React, { useState, useEffect } from 'react';
import { Save, Server, Settings as SettingsIcon } from 'lucide-react';
import { getApiUrl, apiFetch } from '../utils/api';

export default function Settings() {
  const [apiUrl, setApiUrl] = useState('');
  
  useEffect(() => {
    // Load the configured URL from localStorage or fallback to default
    setApiUrl(getApiUrl());
  }, []);

  const handleSave = (e) => {
    e.preventDefault();
    const trimmed = apiUrl.trim();
    if (trimmed) {
      localStorage.setItem('aosApiUrl', trimmed);
      alert('AOS Backend URL updated. The UI will now point to: ' + trimmed);
    } else {
      localStorage.removeItem('aosApiUrl');
      alert('AOS Backend URL cleared. The UI will use the Vite dev proxy (AOS_BACKEND_URL).');
    }
    window.location.reload();
  };

  return (
    <div className="settings-container" style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: '2rem' }}>
      
      {/* AOS Backend Configuration */}
      <section className="glass-panel" style={{ padding: '2rem' }}>
        <h2 style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '1.5rem', borderBottom: '1px solid var(--border-light)', paddingBottom: '1rem' }}>
          <Server className="text-primary" /> Connection Settings
        </h2>
        
        <form onSubmit={handleSave} style={{ display: 'grid', gap: '1rem', maxWidth: '600px' }}>
          <div className="input-group">
            <label>AOS Backend URL</label>
            <input 
              type="text" 
              className="input-base" 
              style={{ borderRadius: 'var(--radius-md)' }}
              value={apiUrl}
              onChange={e => setApiUrl(e.target.value)}
              placeholder="Leave empty to use the Vite proxy (recommended)"
            />
            <small style={{ color: 'var(--text-muted)', marginTop: '0.25rem', display: 'block' }}>
              Empty = requests go through the Vite dev proxy (AOS_BACKEND_URL in vite.config.js).
              Set an absolute URL only to bypass the proxy.
            </small>
          </div>
          
          <div style={{ display: 'flex', justifyContent: 'flex-start', marginTop: '1rem' }}>
            <button type="submit" className="btn btn-primary" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <Save size={18} /> Save Settings
            </button>
          </div>
        </form>
      </section>

    </div>
  );
}
