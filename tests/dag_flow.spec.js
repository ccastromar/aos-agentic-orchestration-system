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

  // 2. We mock the EventSource in the browser window
  await page.addInitScript(() => {
    window.MockEventSource = class MockEventSource {
      constructor(url) {
        this.url = url;
        if (url.includes('stream')) {
          window.__mockEventSourceStream = this;
        } else if (url.includes('events')) {
          window.__mockEventSourceTimeline = this;
        }
        setTimeout(() => {
          if (this.onopen) this.onopen();
        }, 10);
      }
      close() {}
      
      // Helper to dispatch events from playwright
      emit(type, data) {
        if (this.onmessage) {
          this.onmessage({ data: JSON.stringify({ type, data }) });
        }
      }
      emitRaw(dataObj) {
        if (this.onmessage) {
          this.onmessage({ data: JSON.stringify(dataObj) });
        }
      }
    };
    window.EventSource = window.MockEventSource;
  });

  // Navigate to the app
  await page.goto('http://localhost:5173/');

  // Type in the chat panel
  const chatInput = page.locator('input[placeholder*="Ask something"]');
  await chatInput.fill('inicia demo del DAG en paralelo');
  await chatInput.press('Enter');

  // Verify the user message is displayed
  await expect(page.getByText('inicia demo del DAG en paralelo')).toBeVisible();

  // Wait for the backend request to complete and EventSource to be opened
  await page.waitForFunction(() => !!window.__mockEventSourceTimeline);

  // Emit SSE events to simulate backend progress
  await page.evaluate(() => {
    const esTimeline = window.__mockEventSourceTimeline;
    
    esTimeline.emitRaw({ Kind: 'dag_structure', Message: JSON.stringify([
      { id: "check_risk", tool: "banking.aml_risk_check" },
      { id: "check_balance", tool: "banking.core_get_balance" },
      { id: "human_gate", human_approval: "manager_review" },
      { id: "transfer", tool: "banking.payments_transfer" },
      { id: "notify", tool: "banking.send_notification" }
    ]) });
    esTimeline.emitRaw({ Kind: 'tool banking.aml_risk_check', Message: 'ok' });
  });

  // Wait for DAG Visualizer to appear (it renders when there are task events)
  const dagContainer = page.locator('.react-flow');
  await expect(dagContainer).toBeVisible();

  // Simulate human gate
  await page.evaluate(() => {
    window.__mockEventSourceTimeline.emitRaw({ Kind: 'await_human', Message: 'manager_review' });
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
    const esTimeline = window.__mockEventSourceTimeline;
    esTimeline.emitRaw({ Kind: 'human_decision', Message: 'approved', Duration: 'manager_review' });
    esTimeline.emitRaw({ Kind: 'tool banking.payments_transfer', Message: 'ok' });
    esTimeline.emitRaw({ Kind: 'tool banking.send_notification', Message: 'ok' });
  });

  // Verify the human gate node turns success/ok
  await expect(humanGateNode).toHaveClass(/status-success/);
});
