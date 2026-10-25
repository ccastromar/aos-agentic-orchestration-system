import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// WCAG 2.1 AA (the level EN 301 549 requires of public-sector software in the EU), checked with
// axe on every tab of the console, in light and dark themes.

const WCAG = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

async function violations(page) {
  const results = await new AxeBuilder({ page }).withTags(WCAG).analyze();
  return results.violations.map((v) => ({
    rule: v.id,
    impact: v.impact,
    help: v.help,
    nodes: v.nodes.slice(0, 5).map((n) => {
      const contrast = n.any.find((c) => c.id === 'color-contrast')?.data;
      return contrast
        ? `${n.target.join(' ')} — ${contrast.fgColor} on ${contrast.bgColor}: ${contrast.contrastRatio}:1 (needs ${contrast.expectedContrastRatio})`
        : n.target.join(' ');
    }),
  }));
}

for (const scheme of ['light', 'dark']) {
  test.describe(`${scheme} theme`, () => {
    test.use({ colorScheme: scheme });

    test('every tab', async ({ page }) => {
      await page.addInitScript(() => { window.EventSource = class { close() {} }; });
      await page.route('**/api/v1/tasks', (route) => route.fulfill({
        json: [{ id: 'old-task', intent: 'helpdesk.create_ticket', params: { subject: 'Example' }, created_at: '2026-07-18T10:00:00Z' }],
      }));
      await page.goto('/');
      const tabs = page.getByRole('tab');
      await expect(tabs.first()).toBeVisible();
      const found = {};
      for (let i = 0; i < (await tabs.count()); i++) {
        const tab = tabs.nth(i);
        const name = (await tab.textContent())?.trim() || String(i);
        await tab.click();
        await page.waitForLoadState('networkidle');
        // Tabs fade in (framer-motion): measure the settled colours, not a frame of the fade.
        await page.waitForTimeout(800);
        const v = await violations(page);
        if (v.length) found[name] = v;
      }
      expect(found).toEqual({});
    });
  });
}
