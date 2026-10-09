import { test, expect } from '@playwright/test';

test('DAG flow with human approval', async ({ page }) => {
  // We will intercept API requests to mock the backend
  
  // 1. Mock the /ask request
  await page.route('**/ask', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: { 'Access-Control-Allow-Origin': '*' },
      body: JSON.stringify({ id: 'test-task-123', status: 'processing' })
    });
  });

  // Mock pipelines config
  await page.route('**/config/pipelines', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: { 'Access-Control-Allow-Origin': '*' },
      body: JSON.stringify({
        pipeline_dag_demo: {
          name: "pipeline_dag_demo",
          mode: "dag",
          steps: [
            { id: "check_risk", tool: "banking.aml_risk_check" },
            { id: "check_balance", tool: "banking.core_get_balance" },
            { id: "human_gate", human_approval: "manager_review" },
            { id: "transfer", tool: "banking.payments_transfer" },
            { id: "notify", tool: "banking.send_notification" }
          ]
        }
      })
    });
  });

  // 2. Mock the authenticated fetch-based SSE stream used by Timeline
  await page.addInitScript(() => {
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input, options) => {
      const url = typeof input === 'string' ? input : input.url;
      if (!url.includes('/api/v1/events/stream')) return originalFetch(input, options);
      const body = new ReadableStream({
        start(controller) {
          window.__mockTimelineStream = controller;
        }
      });
      return Promise.resolve(new Response(body, {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream' }
      }));
    };
    window.emitTimelineEvent = (data) => {
      window.__mockTimelineStream?.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(data)}\n\n`));
    };
  });

  // Navigate to the app
  await page.goto('http://localhost:5173/');

  // Type in the chat panel
  const chatInput = page.locator('input[placeholder*="Type your request here"]');
  await chatInput.fill('inicia demo del DAG en paralelo');
  await chatInput.press('Enter');

  // Verify the user message is displayed
  await expect(page.getByText('inicia demo del DAG en paralelo')).toBeVisible();

  // Wait for the backend request to complete and authenticated fetch stream to open
  await page.waitForFunction(() => !!window.__mockTimelineStream);

  // Emit SSE events to simulate backend progress
  await page.evaluate(() => {
    window.emitTimelineEvent({ Type: 'agent_event', Payload: { task_id: 'test-task-123', kind: 'dag_structure', message: JSON.stringify([
      { id: "check_risk", tool: "banking.aml_risk_check" },
      { id: "check_balance", tool: "banking.core_get_balance" },
      { id: "human_gate", human_approval: "manager_review" },
      { id: "transfer", tool: "banking.payments_transfer" },
      { id: "notify", tool: "banking.send_notification" }
    ]) } });
    window.emitTimelineEvent({ Type: 'agent_event', Payload: { task_id: 'test-task-123', kind: 'tool banking.aml_risk_check', message: 'ok' } });
  });

  // Wait for DAG Visualizer to appear (it renders when there are task events)
  const dagContainer = page.locator('.react-flow');
  await expect(dagContainer).toBeVisible();

  // Simulate human gate
  await page.evaluate(() => {
    window.emitTimelineEvent({ Type: 'agent_event', Payload: { task_id: 'test-task-123', kind: 'await_human', message: 'manager_review' } });
  });

  // Check that the HumanApprovalModal is visible
  const modalHeader = page.getByText('Human Approval Required');
  await expect(modalHeader).toBeVisible();

  // Check the DAG node status for the human gate
  // Note: we might not know the exact ID, but we know it has class .is-gate
  const humanGateNode = page.locator('.react-flow__node.is-gate');
  // It should have status-paused or status-pending
  await expect(humanGateNode).toHaveClass(/status-paused/);

  // Mock the /task/approve request
  await page.route('**/task/approve*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: { 'Access-Control-Allow-Origin': '*' },
      body: JSON.stringify({ status: 'approved' })
    });
  });

  // Click the Approve button
  await page.getByRole('button', { name: /Approve Action/i }).click();

  // Modal should close
  await expect(modalHeader).not.toBeVisible();

  // Simulate backend continuing
  await page.evaluate(() => {
    window.emitTimelineEvent({ Type: 'agent_event', Payload: { task_id: 'test-task-123', kind: 'human_decision', message: 'approved', duration: 'manager_review' } });
    window.emitTimelineEvent({ Type: 'agent_event', Payload: { task_id: 'test-task-123', kind: 'tool banking.payments_transfer', message: 'ok' } });
    window.emitTimelineEvent({ Type: 'agent_event', Payload: { task_id: 'test-task-123', kind: 'tool banking.send_notification', message: 'ok' } });
  });

  // Verify the human gate node turns success/ok
  await expect(humanGateNode).toHaveClass(/status-success/);
});
