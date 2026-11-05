// A run as a list of steps, built from the task's events. Each step has a tone (ok, danger,
// warn, info, neutral), a kind that picks its icon, a title and an optional detail. Bookkeeping
// events (request received, pipeline started, summarizing) are left out: they say nothing the
// steps around them do not.
const QUIET = new Set(['request', 'started', 'summarizing', 'completed', 'dag_structure', 'reply']);

const stepId = (step) => step.id || step.tool || 'analyst';

// The status of one step of a declared DAG, from the events seen so far.
export const dagStepStatus = (step, events) => {
  const tool = (s) => events.find(e => e.Kind === `tool ${step.tool}` && e.Message === s);
  const approved = events.find(e => e.Kind === 'human_decision' && e.Message === 'approved' && e.Duration === step.human_approval);
  const rejected = events.find(e => e.Kind === 'human_decision' && e.Message === 'rejected' && e.Duration === step.human_approval);
  const awaiting = events.find(e => e.Kind === 'await_human' && e.Message === step.human_approval);
  const summary = events.find(e => e.Agent === 'Analyst' && e.Kind === 'summary');
  if (tool('error') || rejected) return 'error';
  if (tool('ok') || (step.analyst && summary) || approved) return 'success';
  if (awaiting) return 'paused';
  return 'pending';
};

const DAG_TONES = { error: 'danger', success: 'ok', paused: 'warn', pending: 'pending' };

// Whether a declared DAG really branches. Steps without depends_on run in parallel, so it is a
// plain sequence only when each step after the first depends on exactly the one before it.
export const dagBranches = (steps) => steps.some((step, i) => {
  const deps = Array.isArray(step.depends_on) ? step.depends_on : [];
  return i === 0 ? deps.length > 0 : deps.length !== 1 || deps[0] !== stepId(steps[i - 1]);
});

const governanceStep = (evt) => evt.Kind === 'governance_allowed'
  ? { key: 'governance', kind: 'governance', tone: 'ok', title: 'Prudence allowed the run', detail: evt.Message, time: evt.Time }
  : { key: 'governance', kind: 'governance', tone: 'danger', title: 'Prudence blocked the run', detail: evt.Message, time: evt.Time };

// The steps of a declared DAG, in declared order, with the governance decision first.
export const stepsFromDag = (dagSteps, events) => {
  const governance = events.find(e => e.Kind === 'governance_allowed' || e.Kind === 'governance_blocked');
  const steps = governance ? [governanceStep(governance)] : [];
  for (const step of dagSteps) {
    const status = dagStepStatus(step, events);
    const gate = step.human_approval;
    const failure = step.tool && events.find(e => e.Kind === `tool ${step.tool}` && e.Message === 'error');
    steps.push({
      key: stepId(step),
      kind: gate ? 'approval' : step.analyst ? 'analyst' : 'tool',
      tone: DAG_TONES[status],
      title: gate ? `Human approval · ${gate}` : step.tool || 'LLM Analyst',
      detail: status === 'paused' ? 'Waiting for review' : failure ? failure.Duration : undefined,
      params: step.with_params,
    });
  }
  return steps;
};

// The steps of a linear run, in the order its events arrived.
export const stepsFromEvents = (events) => {
  const steps = [];
  const last = (pred) => { for (let i = steps.length - 1; i >= 0; i--) if (pred(steps[i])) return steps[i]; return null; };
  events.forEach((evt, idx) => {
    const key = `${evt.Kind}-${idx}`;
    const kind = evt.Kind || '';
    if (QUIET.has(kind)) return;
    if (kind === 'governance_allowed' || kind === 'governance_blocked') {
      steps.push({ ...governanceStep(evt), key });
    } else if (kind.startsWith('tool ')) {
      const tool = kind.slice(5);
      const failed = evt.Message === 'error';
      // Rebuilt history carries the step's result as its message, not "ok".
      const detail = failed ? evt.Duration : evt.Message && evt.Message !== 'ok' ? evt.Message : undefined;
      steps.push({ key, kind: 'tool', tone: failed ? 'danger' : 'ok', title: tool, detail, time: evt.Time });
    } else if (kind === 'await_human') {
      steps.push({ key, kind: 'approval', tone: 'warn', gate: evt.Message, title: `Human approval · ${evt.Message}`, detail: 'Waiting for review', time: evt.Time });
    } else if (kind === 'human_decision') {
      const gate = last(s => s.kind === 'approval' && s.gate === evt.Duration && s.tone === 'warn');
      const approved = evt.Message === 'approved';
      if (gate) Object.assign(gate, { tone: approved ? 'ok' : 'danger', detail: approved ? 'Approved' : 'Rejected' });
      else steps.push({ key, kind: 'approval', tone: approved ? 'ok' : 'danger', title: `Human approval · ${evt.Duration}`, detail: approved ? 'Approved' : 'Rejected', time: evt.Time });
    } else if (kind === 'await_clarification') {
      steps.push({ key, kind: 'clarification', tone: 'warn', title: 'Missing parameters', detail: evt.Message, time: evt.Time });
    } else if (kind === 'await_callback') {
      steps.push({ key, kind: 'callback', tone: 'info', title: `Waiting for callback · ${evt.Message}`, time: evt.Time });
    } else if (kind === 'callback_received' || kind === 'callback_timeout') {
      const waiting = last(s => s.kind === 'callback' && s.tone === 'info');
      const ok = kind === 'callback_received';
      if (waiting) Object.assign(waiting, { tone: ok ? 'ok' : 'danger', detail: ok ? 'Callback received' : 'Callback deadline exceeded' });
    } else if (kind === 'intent') {
      steps.push({ key, kind: 'intent', tone: 'neutral', title: `Intent · ${evt.Message}`, time: evt.Time });
    } else if (kind === 'summary') {
      steps.push({ key, kind: 'analyst', tone: 'ok', title: 'Summary', detail: evt.Message, time: evt.Time });
    } else if (kind === 'error' || kind === 'agent.failed') {
      steps.push({ key, kind: 'error', tone: 'danger', title: 'Error', detail: evt.Message, time: evt.Time });
    } else {
      steps.push({ key, kind: 'event', tone: 'neutral', title: kind, detail: evt.Message, time: evt.Time });
    }
  });
  return steps;
};
