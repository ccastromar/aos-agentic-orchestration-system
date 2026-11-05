// How a task's status reads in the console: one label and one Badge tone, shared by the chat's
// task cards and the run header so both always say the same thing.
const STATUSES = {
  loading: { label: 'Loading', tone: 'neutral' },
  pending: { label: 'Running', tone: 'info' },
  await_human: { label: 'Awaiting approval', tone: 'warn' },
  await_clarification: { label: 'Needs input', tone: 'warn' },
  ok: { label: 'Completed', tone: 'ok' },
  success: { label: 'Completed', tone: 'ok' },
  completed: { label: 'Completed', tone: 'ok' },
  error: { label: 'Failed', tone: 'danger' },
  rejected: { label: 'Rejected', tone: 'danger' },
  governance_blocked: { label: 'Blocked by Prudence', tone: 'danger' },
  unavailable: { label: 'Not available', tone: 'neutral' },
};

export const taskStatus = (status) => STATUSES[status] || { label: status || 'Unknown', tone: 'neutral' };
