import React from 'react';
import { format } from 'date-fns';
import { AlertTriangle, Check, Circle, Clock, Compass, HelpCircle, ShieldCheck, ShieldX, Sparkles, UserCheck, Webhook, X } from 'lucide-react';

const ICONS = {
  approval: UserCheck,
  callback: Webhook,
  clarification: HelpCircle,
  analyst: Sparkles,
  intent: Compass,
  error: AlertTriangle,
};

const iconFor = (step) => {
  if (step.kind === 'governance') return step.tone === 'ok' ? ShieldCheck : ShieldX;
  if (step.tone === 'pending') return Circle;
  if (step.tone === 'info') return Clock;
  if (ICONS[step.kind]) return ICONS[step.kind];
  return step.tone === 'danger' ? X : Check;
};

// A run's steps, top to bottom: what Prudence decided, then each step and how it went.
const StepList = ({ steps, running }) => (
  <ol className="aos-steps" aria-label="Run steps">
    {steps.map(step => {
      const Icon = iconFor(step);
      return (
        <li key={step.key} className={`aos-step aos-step--${step.tone}`}>
          <span className="aos-step-icon" aria-hidden="true"><Icon size={14} /></span>
          <div className="aos-step-body">
            <div className="aos-step-title">
              <span className={step.kind === 'tool' ? 'aos-mono' : undefined}>{step.title}</span>
              {step.time && <span className="aos-step-time">{format(new Date(step.time), 'HH:mm:ss')}</span>}
            </div>
            {step.detail && <div className="aos-step-detail">{step.detail}</div>}
            {step.params && Object.keys(step.params).length > 0 && (
              <div className="aos-step-detail">
                {Object.entries(step.params).map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`).join(' · ')}
              </div>
            )}
          </div>
        </li>
      );
    })}
    {running && (
      <li className="aos-step aos-step--running">
        <span className="aos-step-icon" aria-hidden="true"><Circle size={14} /></span>
        <div className="aos-step-body aos-step-detail" role="status">{running}</div>
      </li>
    )}
  </ol>
);

export default StepList;
