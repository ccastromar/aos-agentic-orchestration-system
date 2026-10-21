import React, { useState, useEffect } from 'react';
import { Save, Server } from 'lucide-react';
import { Button, Card, Field, Input } from '@chestertown/design';
import { getApiUrl } from '../utils/api';
import MemorySettings from './MemorySettings';

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
    <div className="settings-container" style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: 'var(--sp-6)' }}>
      <Card title={<span className="ct-row"><Server size={20} /> Connection settings</span>}>
        <form onSubmit={handleSave} style={{ maxWidth: 600 }}>
          <Field
            label="AOS backend URL"
            htmlFor="settings-api-url"
            hint="Empty = requests go through the Vite dev proxy (AOS_BACKEND_URL in vite.config.js). Set an absolute URL only to bypass the proxy."
          >
            <Input
              id="settings-api-url"
              value={apiUrl}
              onChange={(e) => setApiUrl(e.target.value)}
              placeholder="Leave empty to use the Vite proxy (recommended)"
            />
          </Field>
          <Button type="submit" variant="primary">
            <Save size={16} /> Save settings
          </Button>
        </form>
      </Card>

      <MemorySettings />
    </div>
  );
}
