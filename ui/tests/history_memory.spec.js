import { test, expect } from '@playwright/test';

test('history rerun creates a new structured execution', async ({ page }) => {
  await page.addInitScript(() => { window.EventSource = class { close() {} }; });
  await page.route('**/api/v1/tasks', route => route.fulfill({ json: [{ id: 'old-task', intent: 'helpdesk.create_ticket', params: { subject: 'Example' }, created_at: '2026-07-18T10:00:00Z' }] }));
  let submitted;
  await page.route('**/ask_structured', route => {
    submitted = route.request().postDataJSON();
    return route.fulfill({ json: { id: 'new-task', status: 'processing' } });
  });
  await page.goto('/');
  await page.getByRole('tab', { name: 'History', exact: true }).click();
  await expect(page.getByText('helpdesk.create_ticket')).toBeVisible();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: 'Re-run', exact: true }).click();
  await expect(page.getByText('Task execution history')).not.toBeVisible();
  expect(submitted).toEqual({ operation: 'helpdesk.create_ticket', params: { subject: 'Example' }, async: true });
});

test('failed rerun remains visible and allows retry', async ({ page }) => {
  await page.route('**/api/v1/tasks', route => route.fulfill({ json: [{ id: 'old-task', intent: 'helpdesk.create_ticket', params: {}, created_at: '2026-07-18T10:00:00Z' }] }));
  await page.route('**/ask_structured', route => route.fulfill({ status: 503 }));
  await page.goto('/');
  await page.getByRole('tab', { name: 'History', exact: true }).click();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: 'Re-run', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('503');
  await expect(page.getByRole('button', { name: 'Re-run', exact: true })).toBeEnabled();
});
