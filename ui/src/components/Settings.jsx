import React, { useState, useEffect } from 'react';
import { Save, GitCommit, Database, Settings as SettingsIcon } from 'lucide-react';
import DAGVisualizer from './DAGVisualizer';

export default function Settings() {
  const [llmConfig, setLlmConfig] = useState({
    engine: '',
    model: '',
    baseUrl: ''
  });
  const [loadingLlm, setLoadingLlm] = useState(true);
  const [savingLlm, setSavingLlm] = useState(false);

  const [pipelines, setPipelines] = useState({});
  const [loadingPipelines, setLoadingPipelines] = useState(true);

  const [selectedPipeline, setSelectedPipeline] = useState(null);

  useEffect(() => {
    fetch('/ui/settings/llm')
      .then(res => res.json())
      .then(data => {
        setLlmConfig(data);
        setLoadingLlm(false);
      })
      .catch(err => {
        console.error('Error fetching LLM config', err);
        setLoadingLlm(false);
      });

    fetch('/ui/pipelines')
      .then(res => res.json())
      .then(data => {
        setPipelines(data);
        setLoadingPipelines(false);
      })
      .catch(err => {
        console.error('Error fetching pipelines', err);
        setLoadingPipelines(false);
      });
  }, []);

  const handleLlmSave = (e) => {
    e.preventDefault();
    setSavingLlm(true);
    fetch('/ui/settings/llm', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(llmConfig)
    })
      .then(res => {
        if (!res.ok) throw new Error('Failed to save');
        return res.json();
      })
      .then(() => {
        alert('LLM Settings updated successfully. Agents will now use the new model.');
        setSavingLlm(false);
      })
      .catch(err => {
        alert('Error saving LLM settings: ' + err.message);
        setSavingLlm(false);
      });
  };

  return (
    <div className="settings-container" style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: '2rem' }}>
      
      {/* LLM Configuration */}
      <section className="glass-panel" style={{ padding: '2rem' }}>
        <h2 style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '1.5rem', borderBottom: '1px solid var(--border-light)', paddingBottom: '1rem' }}>
          <SettingsIcon className="text-primary" /> LLM Configuration
        </h2>
        {loadingLlm ? (
          <p>Loading...</p>
        ) : (
          <form onSubmit={handleLlmSave} style={{ display: 'grid', gap: '1rem', maxWidth: '600px' }}>
            <div className="input-group">
              <label>Engine</label>
              <select 
                className="input-base" 
                style={{ borderRadius: 'var(--radius-md)' }}
                value={llmConfig.engine} 
                onChange={e => setLlmConfig({...llmConfig, engine: e.target.value})}
              >
                <option value="openai">OpenAI (or Compatible)</option>
                <option value="gemini">Google Gemini</option>
                <option value="ollama">Ollama (Local)</option>
              </select>
            </div>
            <div className="input-group">
              <label>Model Name</label>
              <input 
                type="text" 
                className="input-base" 
                style={{ borderRadius: 'var(--radius-md)' }}
                value={llmConfig.model} 
                onChange={e => setLlmConfig({...llmConfig, model: e.target.value})}
                placeholder="e.g. gpt-4o, gemini-2.5-pro, llama3" 
                required
              />
            </div>
            <div className="input-group">
              <label>Base URL</label>
              <input 
                type="text" 
                className="input-base" 
                style={{ borderRadius: 'var(--radius-md)' }}
                value={llmConfig.baseUrl} 
                onChange={e => setLlmConfig({...llmConfig, baseUrl: e.target.value})}
                placeholder="e.g. http://localhost:11434" 
              />
            </div>
            <button type="submit" className="btn btn-primary" disabled={savingLlm} style={{ justifySelf: 'start', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <Save size={16} /> {savingLlm ? 'Saving...' : 'Save Settings'}
            </button>
          </form>
        )}
      </section>

      {/* Pipelines Explorer */}
      <section className="glass-panel" style={{ padding: '2rem' }}>
        <h2 style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '1.5rem', borderBottom: '1px solid var(--border-light)', paddingBottom: '1rem' }}>
          <Database className="text-secondary" /> Registered Pipelines
        </h2>
        {loadingPipelines ? (
          <p>Loading pipelines...</p>
        ) : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: '1rem' }}>
            {Object.entries(pipelines).map(([id, pipeline]) => (
              <div key={id} className="glass-panel" style={{ padding: '1rem', background: 'var(--bg-surface-hover)' }}>
                <h3 style={{ fontSize: '1.1rem', marginBottom: '0.5rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <GitCommit size={16} className="text-accent" /> {id}
                </h3>
                <p style={{ color: 'var(--text-muted)', fontSize: '0.9rem', marginBottom: '1rem' }}>
                  {pipeline.Description || 'No description provided.'}
                </p>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem' }}>
                  <span style={{ color: 'var(--primary)' }}>{pipeline.Steps ? pipeline.Steps.length : 0} steps</span>
                  <button 
                    className="btn btn-secondary" 
                    style={{ padding: '0.25rem 0.75rem', fontSize: '0.8rem' }}
                    onClick={() => setSelectedPipeline(pipeline)}
                  >
                    View Flow
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Pipeline Visualizer Modal */}
      {selectedPipeline && (
        <div style={{
          position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
          background: 'rgba(0,0,0,0.8)', backdropFilter: 'blur(4px)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          zIndex: 1000
        }}>
          <div className="glass-panel" style={{ width: '90%', height: '90%', display: 'flex', flexDirection: 'column' }}>
            <div style={{ padding: '1rem', borderBottom: '1px solid var(--border-light)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h2 style={{ margin: 0 }}>Pipeline Flow: {selectedPipeline.Name}</h2>
              <div style={{ display: 'flex', gap: '1rem' }}>
                <button 
                  className="btn btn-primary" 
                  onClick={async () => {
                    try {
                      // Trigger pipeline by sending its description or name to the NLP intent matcher
                      const reqBody = { message: `Please run: ${selectedPipeline.Description || selectedPipeline.Name}`, lang: 'en' };
                      const res = await fetch('/ask', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(reqBody)
                      });
                      if (!res.ok) throw new Error('API Error');
                      const data = await res.json();
                      
                      alert(`Pipeline started! Task ID: ${data.id}. Switching to the Chat view...`);
                      setSelectedPipeline(null);
                      // Dispatch custom event to trigger app navigation or just alert for now.
                      // Usually, we'd pass a prop, but a global window reload or location change works.
                      window.location.href = `/?taskId=${data.id}`;
                    } catch (e) {
                      alert(`Failed to start pipeline: ${e.message}`);
                    }
                  }}
                  style={{ background: 'linear-gradient(135deg, var(--secondary), #059669)' }}
                >
                  Run Pipeline
                </button>
                <button className="btn btn-secondary" onClick={() => setSelectedPipeline(null)}>Close</button>
              </div>
            </div>
            <div style={{ flex: 1, overflow: 'hidden' }}>
              <DAGVisualizer 
                pipelineSteps={selectedPipeline.Steps || []}
                taskEvents={[]}
                mode={selectedPipeline.Mode}
              />
            </div>
          </div>
        </div>
      )}

    </div>
  );
}
